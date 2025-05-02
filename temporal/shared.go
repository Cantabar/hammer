// temporal/shared.go
package temporal

// ActivityInput holds data for saving results via SaveResultActivity
type ActivityInput struct {
  WorkflowID   string
  Prompt       string
  Plan         string // For future phases
  StepOutputs  string // For future phases (JSON)
  FinalResult  string
  Status       string
  ErrorDetails string
}

type PlanStep struct {
  Step        int      `json:"step"`
  Instruction string  `json:"instruction"`
}

const TaskQueue = "hammer-code-gen-task-queue" // Define task queue centrally
