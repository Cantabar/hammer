package services

import (
	"errors"
	"fmt"
	"strings"
)

type LLMService struct {
	agent Agent // Assume Agent is a defined interface elsewhere in the project
}

func NewLLMService(agent Agent) *LLMService {
	return &LLMService{
		agent: agent,
	}
}

// GenerateCommitMessage takes a git diff as input and uses an agent to generate a git commit message.
func (s *LLMService) GenerateCommitMessage(gitDiff string) (string, error) {
	if gitDiff == "" {
		return "", errors.New("git diff is empty")
	}

	prompt := fmt.Sprintf("Given the following git diff:\n```\n%s\n```\nGenerate a concise git commit message that summarizes the changes.", gitDiff)
	response, err := s.agent.Ask(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate commit message: %w", err)
	}

	// Ensure the commit message is under 50 characters to meet requirements
	if len(response) > 50 {
		return "", errors.New("generated commit message exceeds 50 characters")
	}

	return response, nil
}

// GenerateSemanticCommitPrefix determines the semantic commit prefix based on a git diff.
func (s *LLMService) GenerateSemanticCommitPrefix(gitDiff string) (string, error) {
	if gitDiff == "" {
		return "", errors.New("git diff is empty")
	}

	prompt := fmt.Sprintf("Given the following git diff:\n```\n%s\n```\nDetermine which semantic commit prefix (chore, fix, feat, refactor, test) best matches the changes.", gitDiff)
	response, err := s.agent.Ask(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to determine semantic commit prefix: %w", err)
	}

	// Validate response
	validPrefixes := []string{"chore", "fix", "feat", "refactor", "test"}
	for _, prefix := range validPrefixes {
		if strings.EqualFold(response, prefix) {
			return prefix, nil
		}
	}

	return "", fmt.Errorf("'%s' is not a valid semantic commit prefix", response)
}