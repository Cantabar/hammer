package workflows

import (
	"fmt"
	"myapp/services"
)

// This function orchestrates the generation of a commit message
// based on the current git diff.
func GenerateCommitMessageWorkflow() (string, error) {
	gitService := services.NewGitService()
	llmService := services.NewLLMService()

	// Get current git diff
	diff, err := gitService.GetCurrentDiff()
	if err != nil {
		return "", fmt.Errorf("failed to get current git diff: %w", err)
	}

	// Generate semantic prefix
	prefix, err := llmService.GenerateSemanticCommitPrefix(diff)
	if err != nil {
		return "", fmt.Errorf("failed to generate semantic prefix: %w", err)
	}

	// Generate commit message
	message, err := llmService.GenerateCommitMessage(diff)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	// Combine prefix and message to form the complete commit message
	// Ensure the total length does not exceed 50 characters
	completeMessage := fmt.Sprintf("%s: %s", prefix, message)
	if len(completeMessage) > 50 {
		return "", fmt.Errorf("commit message exceeds 50 characters")
	}

	return completeMessage, nil
}