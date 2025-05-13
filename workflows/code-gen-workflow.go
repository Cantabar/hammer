package workflows

import (
	"fmt"
	"log"
	"services"
)

func GenerateAndCommit() {
	// Get the current git diff
	gitDiff, err := services.GetCurrentDiff()
	if err != nil {
		log.Fatalf("Failed to get current git diff: %v", err)
	}

	// Determine the semantic commit prefix
	semanticPrefix, err := services.GenerateSemanticCommitPrefix(gitDiff)
	if err != nil {
		log.Fatalf("Failed to determine semantic commit prefix: %v", err)
	}

	// Generate the commit message
	commitMessage, err := services.GenerateCommitMessage(gitDiff)
	if err != nil {
		log.Fatalf("Failed to generate commit message: %v", err)
	}

	// Combine the semantic prefix and commit message
	fullCommitMessage := fmt.Sprintf("%s: %s", semanticPrefix, commitMessage)

	// Here we would normally commit the changes using the fullCommitMessage
	// This is a placeholder for the actual git commit command
	fmt.Printf("Committing changes with message: '%s'\n", fullCommitMessage)

	// Example of how the commit might be performed (pseudo-code)
	// err = git.Commit(fullCommitMessage)
	// if err != nil {
	// 	log.Fatalf("Failed to commit changes: %v", err)
	// }
}