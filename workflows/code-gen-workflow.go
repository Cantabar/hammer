package workflows

import (
  "fmt"
  "time"
  "os"

  "hammer/shared"
  "hammer/activities"
  "hammer/services"
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

  // Integration of git_service and llm_service
  gitService := services.NewGitService()
  llmService := services.NewLLMService()

  // --- Loop through steps: Evaluate -> Generate -> Apply ---
  for i, step := range plannedSteps {
    stepNum := i + 1
    logger.Info("Starting step", "Number", stepNum, "Description", step)

    // Get current git diff
    currentDiff, err := gitService.GetCurrentDiff()
    if err != nil {
      logger.Error("Failed to get current git diff.", "Error", err)
      return nil, fmt.Errorf("failed to get current git diff: %w", err)
    }

    // Determine semantic commit prefix
    semanticPrefix, err := llmService.GenerateSemanticCommitPrefix(currentDiff)
    if err != nil {
      logger.Error("Failed to generate semantic commit prefix.", "Error", err)
      return nil, fmt.Errorf("failed to generate semantic commit prefix: %w", err)
    }

    // Generate commit message
    commitMessage, err := llmService.GenerateCommitMessage(currentDiff)
    if err != nil {
      logger.Error("Failed to generate commit message.", "Error", err)
      return nil, fmt.Errorf("failed to generate commit message: %w", err)
    }

    // Ensure the combined commit message is within the 50 characters limit
    fullCommitMessage := fmt.Sprintf("%s: %s", semanticPrefix, commitMessage)
    if len(fullCommitMessage) > 50 {
      fullCommitMessage = fullCommitMessage[:47] + "..."
    }

    // Use the generated commit message for the apply step
    applyInput := shared.WriteAndCommitInput{
        WorkflowID: workflowID,
        Changes:    map[string]string{}, // This should be populated with actual changes
        CommitMessage: fullCommitMessage,
    }
    var commitHash string
    err = workflow.ExecuteActivity(ctx, "WriteFilesAndCommitActivity", applyInput).Get(ctx, &commitHash)
    if err != nil {
      logger.Error("Failed to apply changes and commit.", "Step", stepNum, "Error", err)
      return nil, fmt.Errorf("failed to apply changes for step %d: %w", stepNum, err)
    }
    logger.Info("Successfully applied and committed changes.", "Step", stepNum, "CommitHash", commitHash, "CommitMessage", fullCommitMessage)
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