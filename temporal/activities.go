// temporal/activities.go
package temporal

import (
  "context"
  "database/sql"
  "encoding/json"
  "errors"
  "fmt"
  "log"
  "os"
  "strings"
  "time"

  // Import the embed package
  _ "embed"

  "github.com/sashabaranov/go-openai"
  _ "github.com/mattn/go-sqlite3" // SQLite driver needed for sql.DB type
  "go.temporal.io/sdk/temporal"   // Import temporal for error types
)

// --- Embed Prompt Files ---

//go:embed prompts/planning_agent.txt
var planningAgentSystemPromptBytes []byte

//go:embed prompts/execution_agent.txt
var executionAgentSystemPromptBytes []byte

//go:embed prompts/combine_agent.txt
var combineAgentSystemPromptBytes []byte

// --- Package-level variables to hold loaded prompts ---
var (
  planningAgentSystemPrompt  string
  executionAgentSystemPrompt string
  combineAgentSystemPrompt   string
)

// init function runs once when the package is loaded
func init() {
  log.Println("Loading agent prompts...")
  // Convert byte slices from embedded files to strings
  planningAgentSystemPrompt = string(planningAgentSystemPromptBytes)
  executionAgentSystemPrompt = string(executionAgentSystemPromptBytes)
  combineAgentSystemPrompt = string(combineAgentSystemPromptBytes)

  // Basic validation
  if planningAgentSystemPrompt == "" || executionAgentSystemPrompt == "" || combineAgentSystemPrompt == "" {
    log.Fatal("FATAL: One or more agent prompts failed to load from embedded files.")
  }
  log.Println("Agent prompts loaded successfully.")
}

// --- Package-level DB for Local Activities ---
// This variable will be set by the worker during initialization
var localActivitiesDB *sql.DB

// --- Activities Struct (for Standard Activities) ---
// Holds dependencies needed by standard activities (currently none beyond what's in methods)
type Activities struct {
  DB *sql.DB // Keep DB here in case standard activities need it later
}

// --- Helper for LLM Calls ---
// Centralizes the logic for making calls to the OpenAI API
func (a *Activities) callOpenAI(ctx context.Context, systemPrompt string, userPrompt string, model string, maxTokens int) (string, error) {
  apiKey := os.Getenv("OPENAI_API_KEY")
  if apiKey == "" {
    log.Println("ERROR: OPENAI_API_KEY environment variable not set.")
    return "", temporal.NewNonRetryableApplicationError("API key not configured", "APIKeyError", nil)
  }
  client := openai.NewClient(apiKey)

  messages := []openai.ChatCompletionMessage{
    {Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
    {Role: openai.ChatMessageRoleUser, Content: userPrompt},
  }

  log.Printf("Calling OpenAI model %s with system prompt length %d, user prompt length %d\n", model, len(systemPrompt), len(userPrompt))

  resp, err := client.CreateChatCompletion(
    ctx, // Pass context for cancellation propagation
    openai.ChatCompletionRequest{
      Model:       model,
      Messages:    messages,
      MaxTokens:   maxTokens,
      Temperature: 0.5, // Lower temperature for more predictable agent behavior
    },
  )

  if err != nil {
    log.Printf("OpenAI API request failed: %v\n", err)
    if errors.Is(err, context.Canceled) {
      return "", fmt.Errorf("OpenAI call cancelled: %w", err)
    }
    // Consider checking for other specific, non-retryable errors here
    return "", fmt.Errorf("OpenAI API request failed: %w", err) // Return wrapped error for Temporal retries
  }

  if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
    log.Println("OpenAI Error: Empty response content")
    return "", errors.New("empty response content from OpenAI") // Decide if retryable
  }

  log.Printf("OpenAI call successful. Response length: %d\n", len(resp.Choices[0].Message.Content))
  return resp.Choices[0].Message.Content, nil
}

// --- Agent Activities (Standard - Methods on *Activities) ---

// PlanningAgentActivity uses the loaded planning prompt
func (a *Activities) PlanningAgentActivity(ctx context.Context, userPrompt string) (string, error) {
  log.Println("PlanningAgentActivity: Generating plan for prompt...")
  systemPrompt := planningAgentSystemPrompt
  model := openai.GPT4TurboPreview

  planJSON, err := a.callOpenAI(ctx, systemPrompt, userPrompt, model, 1000)
  if err != nil {
    log.Printf("PlanningAgentActivity Error during OpenAI call: %v\n", err)
    return "", err
  }

  // Basic validation and cleanup
  planJSON = strings.TrimSpace(planJSON)
  if strings.HasPrefix(planJSON, "```json") && strings.HasSuffix(planJSON, "```") {
    planJSON = strings.TrimPrefix(planJSON, "```json")
    planJSON = strings.TrimSuffix(planJSON, "```")
    planJSON = strings.TrimSpace(planJSON)
  } else if strings.HasPrefix(planJSON, "```") && strings.HasSuffix(planJSON, "```") {
        planJSON = strings.TrimPrefix(planJSON, "```")
        planJSON = strings.TrimSuffix(planJSON, "```")
        planJSON = strings.TrimSpace(planJSON)
    }

  // Validate JSON structure
  var planSteps []PlanStep
  if err := json.Unmarshal([]byte(planJSON), &planSteps); err != nil {
    log.Printf("PlanningAgentActivity Error: Failed to unmarshal plan JSON: %v. Response received:\n%s\n", err, planJSON)
    return "", temporal.NewNonRetryableApplicationError(
      fmt.Sprintf("LLM returned invalid JSON plan format: %v", err),
      "InvalidPlanFormat", err, planJSON)
  }
  if len(planSteps) == 0 {
    log.Println("PlanningAgentActivity Warning: Plan is valid JSON but contains zero steps.")
    return "", temporal.NewNonRetryableApplicationError("LLM returned an empty plan", "EmptyPlan", nil, planJSON)
  }

  log.Println("PlanningAgentActivity: Plan generated and validated successfully.")
  return planJSON, nil
}

// ExecutionAgentActivity uses the loaded execution prompt
func (a *Activities) ExecutionAgentActivity(ctx context.Context, stepInstruction string, previousStepOutput string) (string, error) {
  log.Printf("ExecutionAgentActivity: Executing instruction: %s\n", stepInstruction)
  systemPrompt := executionAgentSystemPrompt

  userPrompt := fmt.Sprintf("Instruction:\n%s", stepInstruction)
  if previousStepOutput != "" {
    userPrompt += fmt.Sprintf("\n\n--- Context from previous step ---\n%s\n--- End Context ---", previousStepOutput)
  }

  model := openai.GPT3Dot5Turbo
  stepOutput, err := a.callOpenAI(ctx, systemPrompt, userPrompt, model, 1500)
  if err != nil {
    log.Printf("ExecutionAgentActivity Error during OpenAI call: %v\n", err)
    return "", err
  }

  log.Println("ExecutionAgentActivity: Step executed successfully.")
  return stepOutput, nil
}

// CombineAgentActivity uses the loaded combine prompt
func (a *Activities) CombineAgentActivity(ctx context.Context, originalPrompt string, stepOutputsJSON string) (string, error) {
  log.Println("CombineAgentActivity: Combining step outputs...")
  systemPrompt := combineAgentSystemPrompt

  var stepOutputs []string
  err := json.Unmarshal([]byte(stepOutputsJSON), &stepOutputs)
  if err != nil {
    log.Printf("CombineAgentActivity Error: Failed to parse stepOutputsJSON: %v\n", err)
    return "", temporal.NewNonRetryableApplicationError(
            fmt.Sprintf("internal error: failed to parse step outputs JSON: %v", err),
            "InternalJSONError", err)
  }

  var stepsContextBuilder strings.Builder
  stepsContextBuilder.WriteString("--- Execution Steps Outputs ---\n")
  for i, output := range stepOutputs {
    stepsContextBuilder.WriteString(fmt.Sprintf("--- Output from Step %d ---\n%s\n\n", i+1, output))
  }
  stepsContextBuilder.WriteString("--- End Execution Steps Outputs ---")

  userPrompt := fmt.Sprintf("Original User Prompt:\n```\n%s\n```\n\n%s", originalPrompt, stepsContextBuilder.String())

  model := openai.GPT4TurboPreview
  finalOutput, err := a.callOpenAI(ctx, systemPrompt, userPrompt, model, 2500)
  if err != nil {
    log.Printf("CombineAgentActivity Error during OpenAI call: %v\n", err)
    return "", err
  }

  log.Println("CombineAgentActivity: Final response generated successfully.")
  return finalOutput, nil
}

// --- Database Activities (Local - Functions using package DB) ---

// SaveResultActivity saves the workflow outcome to the database
func SaveResultActivity(ctx context.Context, input ActivityInput) error {
  log.Printf("SaveResultActivity (func): Saving data for WorkflowID %s (Status: %s)\n", input.WorkflowID, input.Status)
  if localActivitiesDB == nil {
    log.Println("SaveResultActivity Error: Package-level database connection (localActivitiesDB) is nil")
    return errors.New("database connection is not initialized for local activities")
  }

  query := `
    INSERT INTO results (workflow_id, prompt, plan, step_outputs, final_result, status, error_details, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    ON CONFLICT(workflow_id) DO UPDATE SET
        prompt       = COALESCE(excluded.prompt, results.prompt),
        plan         = COALESCE(excluded.plan, results.plan),
        step_outputs = COALESCE(excluded.step_outputs, results.step_outputs),
        final_result = COALESCE(excluded.final_result, results.final_result),
        status       = excluded.status,
        error_details= excluded.error_details,
        updated_at   = excluded.updated_at;
    `
  now := time.Now().UTC()

  _, err := localActivitiesDB.ExecContext(ctx, query,
    input.WorkflowID, input.Prompt, input.Plan, input.StepOutputs, input.FinalResult, input.Status, input.ErrorDetails, // INSERT values
    input.Prompt, input.Plan, input.StepOutputs, input.FinalResult, input.Status, input.ErrorDetails, now, // excluded.* values for UPDATE
  )

  if err != nil {
    log.Printf("SaveResultActivity: Error saving result for WorkflowID %s: %v\n", input.WorkflowID, err)
    return fmt.Errorf("failed to save result to database: %w", err)
  }

  log.Printf("SaveResultActivity: Successfully saved/updated result for WorkflowID %s\n", input.WorkflowID)
  return nil
}

// CreatePendingRecordActivity creates the initial DB record (if it doesn't exist)
func CreatePendingRecordActivity(ctx context.Context, workflowID, prompt string) error {
  log.Printf("CreatePendingRecordActivity (func): Creating initial record for WorkflowID %s\n", workflowID)
  if localActivitiesDB == nil {
    log.Println("CreatePendingRecordActivity Error: Package-level database connection (localActivitiesDB) is nil")
    return errors.New("database connection is not initialized for local activities")
  }
  query := `INSERT OR IGNORE INTO results (workflow_id, prompt, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
  now := time.Now().UTC()
  _, err := localActivitiesDB.ExecContext(ctx, query, workflowID, prompt, "PENDING", now, now)
  if err != nil {
    log.Printf("CreatePendingRecordActivity: Error creating pending record for WorkflowID %s: %v\n", workflowID, err)
    return fmt.Errorf("failed to create pending record: %w", err)
  }
  log.Printf("CreatePendingRecordActivity: Initial record ensured for WorkflowID %s\n", workflowID)
  return nil
}
