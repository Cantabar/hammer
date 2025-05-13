package services

import (
	"bytes"
	"os/exec"
)

// GitService provides functionalities to interact with Git.
type GitService struct{}

// GetCurrentDiff returns the git diff of the current working branch.
func (gs *GitService) GetCurrentDiff() (string, error) {
	cmd := exec.Command("git", "diff")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}