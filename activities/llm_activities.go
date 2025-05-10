package activities

import (
  "context"
  "fmt"

  "hammer/services"
  "hammer/shared"
)

const (
  ActivityName_PlanSteps      = "PlanStepsActivity"
  ActivityName_EvaluateFiles  = "EvaluateFilesActivity"
  ActivityName_GenerateCode   = "GenerateCodeActivity"
)

type LLMActivities struct {
  LLMService *services.LLMService
}

func NewLLMActivities(llmService *services.LLMService) *LLMActivities {
  return &LLMActivities{LLMService: llmService}
}

func (a *LLMActivities) PlanStepsActivity(ctx context.Context, userPrompt string) ([]string, error) {
  steps, err := a.LLMService.PlanSteps(ctx, userPrompt)
  if err != nil {
    return nil, fmt.Errorf("PlanStepsActivity failed: %w", err)
  }
  return steps, nil
}

func (a *LLMActivities) EvaluateFilesActivity(ctx context.Context, input shared.EvaluateFilesActivityInput) (*shared.EvaluateFilesActivityResult, error) {
  relevantFiles, err := a.LLMService.EvaluateRelevantFiles(ctx, input.StepDescription, input.AllFiles)
  if err != nil {
    return nil, fmt.Errorf("EvaluateFilesActivity failed: %w", err)
  }
  return &shared.EvaluateFilesActivityResult{RelevantFiles: relevantFiles}, nil
}

func (a *LLMActivities) GenerateCodeActivity(ctx context.Context, input shared.GenerateCodeActivityInput) (*shared.GenerateCodeActivityResult, error) {
  generatedFiles, err := a.LLMService.GenerateCodeChanges(ctx, input.StepDescription, input.RelevantFilesContent, input.OriginalUserPrompt)
  if err != nil {
    return nil, fmt.Errorf("GenerateCodeActivity failed: %w", err)
  }
  return &shared.GenerateCodeActivityResult{GeneratedFiles: generatedFiles}, nil
}
