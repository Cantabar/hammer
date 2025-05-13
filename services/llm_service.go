package services

import (
	"fmt"
	"strings"
)

// GenerateCommitMessage generates a git commit message based on the provided git diff
func GenerateCommitMessage(gitDiff string) string {
	// Simulating a call to an agent to generate a commit message based on the git diff
	// This is a placeholder implementation
	commitMessage := "Updates detected in module" // Example generated message

	// Ensure the commit message is under 50 characters
	if len(commitMessage) > 50 {
		commitMessage = commitMessage[:47] + "..."
	}

	return commitMessage
}

// GenerateSemanticCommitPrefix determines the semantic commit prefix based on the provided git diff
func GenerateSemanticCommitPrefix(gitDiff string) string {
	// Simulating a call to an agent to determine the semantic commit prefix
	// This is a placeholder implementation
	prefixes := []string{"chore", "fix", "feat", "refactor", "test"}
	selectedPrefix := prefixes[0] // Example: selecting "chore" as the prefix

	return selectedPrefix
}