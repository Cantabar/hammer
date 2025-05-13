package workflows

import (
	"fmt"
	"services"
	"strings"
)

func GenerateCommit() {
	gitService := services.GetCurrentDiff()
	llmServicePrefix := services.GenerateSemanticCommitPrefix(gitService)
	llmServiceMessage := services.GenerateCommitMessage(gitService)

	// Combine prefix and message ensuring the total length does not exceed 50 characters
	fullMessage := llmServicePrefix + ": " + llmServiceMessage
	if len(fullMessage) > 50 {
		trimSize := 50 - len(llmServicePrefix) - 2 // 2 for ": "
		if trimSize < 0 {
			fmt.Println("Error: Prefix too long.")
			return
		}
		fullMessage = llmServicePrefix + ": " + llmServiceMessage[:trimSize]
	}

	fmt.Println("Generated Commit Message: ", fullMessage)
}