// temporal/workflows.go
package temporal

import (
  "encoding/json"
  "errors"
  "fmt"
  "time"

  "go.temporal.io/sdk/temporal"
  "go.temporal.io/sdk/workflow"
)

// CodeGenerationWorkflow orchestrates the multi-agent process for Phase 4
func CodeGenerationWorkflow(ctx workflow.Context, prompt string) (string, error) {
  logger := workflow.GetLogger(ctx)
  logger.Info("CodeGenerationWorkflow (Phase 4) started", "Prompt", prompt)

  workflowInfo := workflow.GetInfo(ctx)
  workflowID := workflowInfo.WorkflowExecution.ID

  // --- Setup Activity Options ---
  localActivityOpts := workflow.LocalActivityOptions{
    ScheduleToCloseTimeout: 15 * time.Second,
    RetryPolicy: &temporal.RetryPolicy{
      InitialInterval:    time.Second,
      BackoffCoefficient: 2.0,
      MaximumInterval:    time.Minute,
      MaximumAttempts:    5,
    },
  }
  agentActivityOpts := workflow.ActivityOptions{
    StartToCloseTimeout: 3 * time.Minute,
    RetryPolicy: &temporal.RetryPolicy{
      InitialInterval:        time.Second,
      BackoffCoefficient:     2.0,
      MaximumInterval:        time.Minute,
      MaximumAttempts:        3,
      NonRetryableErrorTypes: []string{"APIKeyError", "InvalidPlanFormat", "EmptyPlan", "InternalJSONError"}, // Add non-retryable types
    },
    HeartbeatTimeout: 45 * time.Second,
  }
  combineActivityOpts := workflow.ActivityOptions{
    StartToCloseTimeout: 4 * time.Minute,
    RetryPolicy:         agentActivityOpts.RetryPolicy, // Reuse same retry policy
    HeartbeatTimeout:    45 * time.Second,
  }

  // Apply options to contexts
  localCtx := workflow.WithLocalActivityOptions(ctx, localActivityOpts)
  agentCtx := workflow.WithActivityOptions(ctx, agentActivityOpts)
  combineCtx := workflow.WithActivityOptions(ctx, combineActivityOpts)

  // Activity stubs - declare a nil pointer of the type that has the *methods*
  // This is used only for the compiler/reflection to find the method reference.
  var actsForStandard *Activities

  // --- State Variables ---
  currentStatus := "PENDING"
  var planningErr, executionErr, combineErr, saveErr error // Track errors
  var planJSON string = ""
  var stepOutputsJSON string = "[]"
  var finalResult string = ""

  // --- Defer saving final status ---
  defer func() {
    finalStatus := ""
    finalErrorDetails := ""
    primaryError := executionErr // Prioritize errors
    if primaryError == nil { primaryError = combineErr }
    if primaryError == nil { primaryError = planningErr }

    if panicked := recover(); panicked != nil {
      logger.Error("Workflow panicked", "PanicError", panicked)
      finalStatus = "FAILED"
      finalErrorDetails = fmt.Sprintf("Workflow panic: %v", panicked)
      panic(panicked) // Re-panic is required by SDK
    } else if primaryError != nil {
      logger.Error("Workflow finished with error", "Error", primaryError)
      finalStatus = "FAILED"
      finalErrorDetails = primaryError.Error()
      if saveErr != nil {
        logger.Error("SaveResultActivity also failed during error reporting.", "SaveError", saveErr)
      }
    } else if saveErr != nil {
      logger.Error("Final SaveResultActivity failed after successful execution.", "SaveError", saveErr)
      finalStatus = "FAILED"
      finalErrorDetails = fmt.Sprintf("Workflow logic successful, but final state save failed: %v", saveErr)
    } else {
      logger.Info("Workflow finished successfully.")
      finalStatus = "COMPLETED"
    }

    saveInput := ActivityInput{
      WorkflowID:   workflowID,
      Prompt:       prompt,
      Plan:         planJSON,
      StepOutputs:  stepOutputsJSON,
      FinalResult:  finalResult, // Contains combined result on success
      Status:       finalStatus,
      ErrorDetails: finalErrorDetails,
    }

    // Use disconnected context for final save attempt
    disconnectedCtx, _ := workflow.NewDisconnectedContext(ctx)
    localDisconnectedCtx := workflow.WithLocalActivityOptions(disconnectedCtx, localActivityOpts)
    // Call the function directly
    finalSaveAttemptErr := workflow.ExecuteLocalActivity(localDisconnectedCtx, SaveResultActivity, saveInput).Get(localDisconnectedCtx, nil)
    if finalSaveAttemptErr != nil {
      logger.Error("Deferred final save attempt failed.", "Error", finalSaveAttemptErr)
    } else {
      fmt.Printf("Deferred final save successful for WorkflowID %s with Status %s\n", workflowID, finalStatus)
    }
  }() // End of deferred function

  // --- Workflow Steps ---

  // 1. Create Initial Pending Record
  currentStatus = "DB_INIT"
  // Call the function directly
  saveErr = workflow.ExecuteLocalActivity(localCtx, CreatePendingRecordActivity, workflowID, prompt).Get(localCtx, nil)
  if saveErr != nil {
    logger.Warn("CreatePendingRecordActivity failed, continuing...", "Error", saveErr)
    saveErr = nil // Reset saveErr as we decided to continue
  }

  // 2. Planning Step
  logger.Info("Executing Planning Agent Activity...")
  currentStatus = "PLANNING"
  // Call the function directly
  saveErr = workflow.ExecuteLocalActivity(localCtx, SaveResultActivity, ActivityInput{WorkflowID: workflowID, Status: currentStatus, Prompt: prompt}).Get(localCtx, nil)
  if saveErr != nil { logger.Warn("Failed to save PLANNING status", "Error", saveErr); saveErr = nil; }

  // Execute the standard activity method
  err := workflow.ExecuteActivity(agentCtx, actsForStandard.PlanningAgentActivity, prompt).Get(agentCtx, &planJSON)
  if err != nil {
    logger.Error("PlanningAgentActivity failed.", "Error", err)
    planningErr = err // Store primary error
    return "", planningErr // Exit workflow (defer will save)
  }
  logger.Info("Planning complete.", "PlanJSONLength", len(planJSON))
  // Save the plan
  // Call the function directly
  saveErr = workflow.ExecuteLocalActivity(localCtx, SaveResultActivity, ActivityInput{WorkflowID: workflowID, Plan: planJSON, Status: currentStatus}).Get(localCtx, nil)
  if saveErr != nil { logger.Error("Failed to save plan state", "Error", saveErr); saveErr = nil; }

  // 3. Execution Steps
  logger.Info("Parsing plan...")
  var planSteps []PlanStep
  // Use workflow.SideEffect if unmarshalling external data, but internal JSON is deterministic
  err = json.Unmarshal([]byte(planJSON), &planSteps)
  if err != nil {
    logger.Error("Failed to parse plan JSON.", "Error", err, "PlanJSON", planJSON)
    planningErr = fmt.Errorf("failed to parse plan JSON: %w", err)
    return "", planningErr
  }

  if len(planSteps) == 0 {
    logger.Warn("Plan contains zero steps.")
    planningErr = errors.New("generated plan was empty")
    return "", planningErr
  }
  logger.Info(fmt.Sprintf("Plan parsed successfully. Found %d steps.", len(planSteps)))

  executionOutputs := make([]string, 0, len(planSteps))
  previousOutput := ""

  for i, step := range planSteps {
    stepNum := i + 1
    logger.Info(fmt.Sprintf("Executing Step %d/%d: %s", stepNum, len(planSteps), step.Instruction))
    currentStatus = fmt.Sprintf("EXECUTING_STEP_%d", stepNum)
    // Save current status
    // Call the function directly
    saveErr = workflow.ExecuteLocalActivity(localCtx, SaveResultActivity, ActivityInput{WorkflowID: workflowID, Status: currentStatus}).Get(localCtx, nil)
    if saveErr != nil { logger.Error("Failed to save execution step status", "Step", stepNum, "Error", saveErr); saveErr = nil; }

    var currentOutput string
    // Execute the standard activity method
    err = workflow.ExecuteActivity(agentCtx, actsForStandard.ExecutionAgentActivity, step.Instruction, previousOutput).Get(agentCtx, &currentOutput)
    if err != nil {
      logger.Error(fmt.Sprintf("ExecutionAgentActivity failed for Step %d.", stepNum), "Error", err)
      executionErr = fmt.Errorf("step %d execution failed: %w", stepNum, err)
      return "", executionErr
    }

    executionOutputs = append(executionOutputs, currentOutput)
    previousOutput = currentOutput
    logger.Info(fmt.Sprintf("Step %d completed.", stepNum))

    // Optionally save intermediate results - commented out for brevity
    // tempOutputsJSONBytes, _ := json.Marshal(executionOutputs)
    // saveErr = workflow.ExecuteLocalActivity(localCtx, SaveResultActivity, ActivityInput{WorkflowID: workflowID, StepOutputs: string(tempOutputsJSONBytes), Status: currentStatus}).Get(localCtx, nil)
    // if saveErr != nil { logger.Error("Failed to save intermediate step outputs", "Step", stepNum, "Error", saveErr); saveErr = nil; }
  }
  logger.Info("All execution steps completed successfully.")

  // 4. Combine Step
  logger.Info("Executing Combine Agent Activity...")
  currentStatus = "COMBINING"
  // Save status before combine
  // Call the function directly
  saveErr = workflow.ExecuteLocalActivity(localCtx, SaveResultActivity, ActivityInput{WorkflowID: workflowID, Status: currentStatus}).Get(localCtx, nil)
  if saveErr != nil { logger.Error("Failed to save COMBINING status", "Error", saveErr); saveErr = nil; }

  // Marshal executionOutputs to JSON string
  stepOutputsJSONBytes, err := json.Marshal(executionOutputs) // Deterministic
  if err != nil {
    logger.Error("Failed to marshal step outputs for combiner.", "Error", err)
    executionErr = fmt.Errorf("internal error: failed to marshal step outputs: %w", err)
    return "", executionErr
  }
  stepOutputsJSON = string(stepOutputsJSONBytes) // Update state variable

  // Execute the standard activity method
  err = workflow.ExecuteActivity(combineCtx, actsForStandard.CombineAgentActivity, prompt, stepOutputsJSON).Get(combineCtx, &finalResult)
  if err != nil {
    logger.Error("CombineAgentActivity failed.", "Error", err)
    combineErr = err // Store primary error
    return "", combineErr
  }
  logger.Info("Combining step completed successfully.")

  // 5. Final state ("COMPLETED") and results will be saved by the deferred function

  logger.Info("Workflow logic sequence completed without errors.", "WorkflowID", workflowID)
  return "Workflow completed successfully", nil // Workflow's return value on success path
}
