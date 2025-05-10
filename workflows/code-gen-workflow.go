package workflows

import (
  "fmt"
  "time"
  "os"

  "hammer/shared"
  "hammer/activities"
  "go.temporal.io/sdk/workflow"
  "go.temporal.io/sdk/temporal"
)

// CodeGenWorkflow orchestrates the multi-agent code generation process.
func CodeGenWorkflow(ctx workflow.Context, input shared.WorkflowInput) (*shared.WorkflowOutput, error) {
  // Workflow options (timeouts, retries)
  ao := workflow.ActivityOptions{
    StartToCloseTimeout: time.Minute * 5, // Adjust as needed for LLM calls
    RetryPolicy: &temporal.RetryPolicy{
      InitialInterval: time.Second,
      BackoffCoefficient: 2.0,
      MaximumInterval: time.Minute * 2,
      MaximumAttempts: 4, // Retry LLM/Git calls a few times
    },
  }
  ctx = workflow.WithActivityOptions(ctx, ao)

  logger := workflow.GetLogger(ctx)
  logger.Info("CodeGenWorkflow started", "Prompt", input.UserPrompt, "RepoURL", input.RepoURL)
  workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID

  gitUsername := os.Getenv("GIT_USERNAME")
  gitPassword := os.Getenv("GIT_PAT")

  if gitUsername == "" || gitPassword == "" {
    logger.Warn("GIT_USERNAME OR GIT_PAT not set in workflow enviornment.")
    // return nil, workflow.NewApplicationError("Configuration error: Git credentials not provided", "GIT_CREDS_MISSING", nil)
  }

  gitCreds := shared.GitCredentials{
    Username: gitUsername,
    Password: gitPassword,
  }

  // Activity input structs need the WorkflowID
  initGitInput := shared.InitGitActivityInput{
    WorkflowID:   workflowID,
    RepoURL:      input.RepoURL,
    Credentials:  gitCreds,
  }
  err := workflow.ExecuteActivity(ctx, "InitGitActivity", initGitInput).Get(ctx, nil)
  if err != nil {
      logger.Error("Failed to initialize Git repository for workflow.", "Error", err)
      return nil, fmt.Errorf("git initialization failed: %w", err)
  }
  // Ensure cleanup happens even if workflow fails mid-way
  defer func() {
    deferCtx := ctx
    cleanupInput := shared.CleanupGitActivityInput{WorkflowID: workflowID}
    err := workflow.ExecuteActivity(deferCtx, activities.ActivityName_CleanupGit, cleanupInput).Get(deferCtx, nil)
    // Log error, but don't fail the workflow if cleanup fails
    if err != nil {
        logger.Error("Failed to cleanup Git repository for workflow.", "Error", err)
    } else {
         logger.Info("Successfully cleaned up Git service for workflow.")
    }
  }()

  // 1. Planning Agent
  var plannedSteps []string
  planActivityInput := input.UserPrompt // Direct input for this one
  err = workflow.ExecuteActivity(ctx, "PlanStepsActivity", planActivityInput).Get(ctx, &plannedSteps)
  if err != nil {
    logger.Error("Planning activity failed.", "Error", err)
    return nil, fmt.Errorf("planning failed: %w", err)
  }
  if len(plannedSteps) == 0 {
      logger.Warn("Planning resulted in zero steps.")
      // Decide whether to stop or continue (maybe create branch anyway?)
      return nil, fmt.Errorf("planning resulted in zero steps")
  }
   logger.Info("Planning complete.", "Steps", plannedSteps)


  // --- Loop through steps: Evaluate -> Generate -> Apply ---
  for i, step := range plannedSteps {
    stepNum := i + 1
    logger.Info("Starting step", "Number", stepNum, "Description", step)

    // 2a. Evaluation Agent - Get all current files first
    listFilesInput := shared.ListFilesGitActivityInput{WorkflowID: workflowID}
    var allFiles []string
    err = workflow.ExecuteActivity(ctx, "ListFilesGitActivity", listFilesInput).Get(ctx, &allFiles)
     if err != nil {
         logger.Error("Failed to list files for evaluation.", "Step", stepNum, "Error", err)
         return nil, fmt.Errorf("failed to list files for step %d: %w", stepNum, err)
     }

    // Now evaluate which files are relevant
    evalInput := shared.EvaluateFilesActivityInput{
      StepDescription: step,
      AllFiles:        allFiles,
    }
    var evalResult shared.EvaluateFilesActivityResult // Pointer removed, Get populates directly
    err = workflow.ExecuteActivity(ctx, "EvaluateFilesActivity", evalInput).Get(ctx, &evalResult)
    if err != nil {
      logger.Error("Evaluation activity failed.", "Step", stepNum, "Error", err)
      return nil, fmt.Errorf("evaluation failed for step %d: %w", stepNum, err)
    }
    logger.Info("Evaluation complete.", "Step", stepNum, "RelevantFiles", evalResult.RelevantFiles)


    // 2b. Read Relevant Files (using Git Activity)
    readFileContent := make(map[string]string) // Default to empty map
    if len(evalResult.RelevantFiles) > 0 {
        readFilesInput := shared.ReadFilesGitActivityInput{
            WorkflowID: workflowID,
            FilePaths: evalResult.RelevantFiles,
        }
        err = workflow.ExecuteActivity(ctx, "ReadFilesGitActivity", readFilesInput).Get(ctx, &readFileContent)
        if err != nil {
            logger.Error("Failed to read relevant files.", "Step", stepNum, "Files", evalResult.RelevantFiles, "Error", err)
            return nil, fmt.Errorf("failed to read files for step %d: %w", stepNum, err)
        }
         logger.Info("Successfully read relevant files.", "Step", stepNum, "FileCount", len(readFileContent))
    } else {
         logger.Info("No relevant files to read for this step.", "Step", stepNum)
    }


    // 2c. Code Generation Agent
    genCodeInput := shared.GenerateCodeActivityInput{
      StepDescription:      step,
      RelevantFilesContent: readFileContent,
      OriginalUserPrompt:   input.UserPrompt, // Provide original context
    }
    var genCodeResult shared.GenerateCodeActivityResult // Pointer removed
    err = workflow.ExecuteActivity(ctx, "GenerateCodeActivity", genCodeInput).Get(ctx, &genCodeResult)
    if err != nil {
      logger.Error("Code generation activity failed.", "Step", stepNum, "Error", err)
      return nil, fmt.Errorf("code generation failed for step %d: %w", stepNum, err)
    }
     if len(genCodeResult.GeneratedFiles) == 0 {
         logger.Info("Code generation produced no file changes for this step.", "Step", stepNum)
         // Continue to the next step without attempting to commit
         continue
     }
    logger.Info("Code generation complete.", "Step", stepNum, "FilesChanged", len(genCodeResult.GeneratedFiles))


    // 2d. Apply Changes (Write files and commit via Git Activity)
    commitMsg := fmt.Sprintf("AI Agent: Apply step %d/%d: %s", stepNum, len(plannedSteps), step)
    // Ensure commit message is concise if step description is long
    if len(commitMsg) > 100 {
        commitMsg = commitMsg[:97] + "..."
    }

    applyInput := shared.WriteAndCommitInput{
        WorkflowID: workflowID,
        Changes: genCodeResult.GeneratedFiles,
        CommitMessage: commitMsg,
    }
    var commitHash string
    err = workflow.ExecuteActivity(ctx, "WriteFilesAndCommitActivity", applyInput).Get(ctx, &commitHash)
    if err != nil {
      logger.Error("Failed to apply changes and commit.", "Step", stepNum, "Error", err)
      return nil, fmt.Errorf("failed to apply changes for step %d: %w", stepNum, err)
    }
    logger.Info("Successfully applied and committed changes.", "Step", stepNum, "CommitHash", commitHash)
  } // End of steps loop


  // 3. Create Final Branch
  // Generate a unique branch name
  branchName := fmt.Sprintf("%sai-%s", os.Getenv("BRANCH_PREFIX"), workflow.GetInfo(ctx).WorkflowExecution.RunID) // Use RunID for uniqueness
  logger.Info("Attempting to create final branch.", "BranchName", branchName)

  createBranchInput := shared.CreateBranchInput{
      WorkflowID: workflowID,
      BranchName: branchName,
  }
  err = workflow.ExecuteActivity(ctx, "CreateBranchActivity", createBranchInput).Get(ctx, nil)
  if err != nil {
      logger.Error("Failed to create final branch.", "BranchName", branchName, "Error", err)
      // Decide: should this be a fatal error for the workflow? Probably.
      return nil, fmt.Errorf("failed to create branch %s: %w", branchName, err)
  }

  if gitUsername != "" && gitPassword != "" {
    logger.Info("Attempting to push branch to remote.", "BranchName", branchName)
    pushInput := shared.PushBranchActivityInput{
      WorkflowID: workflowID,
      BranchName: branchName,
    }
    err = workflow.ExecuteActivity(ctx, activities.ActivityName_PushBranch, pushInput).Get(ctx, nil)
    if err != nil {
      logger.Error("Push branch activity failed.", "BranchName", branchName, "Error", err)
      return &shared.WorkflowOutput{
        BranchName: branchName,
        Message:    fmt.Sprintf("Code generated on branch '%s', but failed to push to remote: %v", branchName, err),
      }, nil
    }
    logger.Info("Successfully pushed branch to remote.", "BranchName", branchName)
  } else {
    logger.Info("Skipping push operation as Git credentials were not provided.")
  }

  logger.Info("CodeGenWorkflow completed successfully.", "FinalBranch", branchName)
  finalMessage := fmt.Sprintf("Successfully generated code and created branch '%s'", branchName)
  if gitUsername != "" && gitPassword != "" {
    finalMessage += " and pushed to remote."
  } else {
    finalMessage += ". Push skipped (no credentials)."
  }
  return &shared.WorkflowOutput{
    BranchName: branchName,
    Message:    finalMessage,
  }, nil
}
