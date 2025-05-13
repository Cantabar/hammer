package workflows

import (
	"fmt"
	"services" // Assuming services is the package where git_service.go and llm_service.go are located
)

// CodeGenWorkflow represents a workflow for generating code.
type CodeGenWorkflow struct {
	gitService  *services.GitService
	llmService  *services.LLMService
}

// NewCodeGenWorkflow creates a new instance of CodeGenWorkflow.
func NewCodeGenWorkflow(gitService *services.GitService, llmService *services.LLMService) *CodeGenWorkflow {
	return &CodeGenWorkflow{
		gitService:  gitService,
		llmService:  llmService,
	}
}

// Execute runs the workflow to generate a commit message for each commit.
func (w *CodeGenWorkflow) Execute() {
	diff, err := w.gitService.GetCurrentDiff()
	if err != nil {
		fmt.Println("Error getting current diff:", err)
		return
	}

	prefix, err := w.llmService.GenerateSemanticCommitPrefix(diff)
	if err != nil {
		fmt.Println("Error determining semantic commit prefix:", err)
		return
	}

	message, err := w.llmService.GenerateCommitMessage(diff)
	if err != nil {
		fmt.Println("Error generating commit message:", err)
		return
	}

	// Ensure the total commit message length does not exceed 50 characters
	commitMessage := fmt.Sprintf("%s: %s", prefix, message)
	if len(commitMessage) > 50 {
		fmt.Println("Commit message exceeds 50 characters limit")
		return
	}

	fmt.Println("Generated commit message:", commitMessage)
}