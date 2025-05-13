package services

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"hammer/shared"
)

type GitService struct {
	repo      *git.Repository
	fs        billy.Filesystem
	username  string
	password  string
}

func NewGitService(repoURL string, creds shared.GitCredentials) (*GitService, error) {
	log.Printf("Cloning repository %s into memory...", repoURL)
	fs := memfs.New()
	if fs == nil {
		return nil, fmt.Errorf("memfs.New() returned nil unexpectedly")
	}
	storer := memory.NewStorage()

	cloneOpts := &git.CloneOptions{
		URL:      repoURL,
		Progress: nil,
		Depth:    1,
	}
	if creds.Username != "" || creds.Password != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: creds.Username,
			Password: creds.Password,
		}
		log.Println("Using provided credentials for clone.")
	}

	repo, err := git.Clone(storer, fs, cloneOpts)
	if err != nil {
		if strings.Contains(err.Error(), "authentication required") || strings.Contains(err.Error(), "authorization failed") {
			log.Printf("Cloning failed due to potential authentication error. Check URL, username, and PAT permissions. Error: %v", err)
			return nil, fmt.Errorf("repository cloning failed: authentication required - check credentials/permissions: %w", err)
		}
		return nil, fmt.Errorf("failed to clone repo: %w", err)
	}
	log.Println("Repository cloned successfully.")

	return &GitService{
		repo:     repo,
		fs:       fs,
		username: creds.Username,
		password: creds.Password,
	}, nil
}

func (s *GitService) GetCurrentDiff() (string, error) {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}
	patch, err := worktree.Diff()
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}
	buf := bytes.NewBuffer(nil)
	if err := patch.Encode(buf); err != nil {
		return "", fmt.Errorf("failed to encode diff: %w", err)
	}
	return buf.String(), nil
}

func (s *GitService) ListFiles() ([]string, error) {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}
	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}
	var files []string
	idx, err := s.repo.Storer.Index()
	if err != nil {
		return nil, fmt.Errorf("failed to get index: %w", err)
	}
	trackedFiles := make(map[string]struct{})
	for _, entry := range idx.Entries {
		files = append(files, entry.Name)
		trackedFiles[entry.Name] = struct{}{}
	}
	for filePath := range status {
		if status.IsUntracked(filePath) {
			if _, exists := trackedFiles[filePath]; !exists {
				files = append(files, filePath)
			}
		}
	}
	var filteredFiles []string
	for _, file := range files {
		if !strings.HasPrefix(file, ".git/") && file != ".git" {
			filteredFiles = append(filteredFiles, file)
		}
	}
	return filteredFiles, nil
}

func (s *GitService) ReadFile(filePath string) (string, error) {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	fs := worktree.Filesystem

	file, err := fs.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("file '%s' not found in worktree: %w", filePath, err)
		}
		return "", fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}

	return string(content), nil
}

func (s *GitService) WriteFile(filePath string, content string) error {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	fs := worktree.Filesystem
	parts := strings.Split(filePath, "/")
	if len(parts) > 1 {
		dirPath := strings.Join(parts[:len(parts)-1], "/")
		if dirPath != "" {
			if err := fs.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory for '%s': %w", filePath, err)
			}
		}
	}
	file, err := fs.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create/open file '%s' for writing: %w", filePath, err)
	}
	_, writeErr := file.Write([]byte(content))
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to write to file '%s': %w", filePath, writeErr)
	}
	if closeErr != nil {
		log.Printf("Warning: failed to close file '%s' after writing: %v", filePath, closeErr)
	}
	_, err = worktree.Add(filePath)
	if err != nil {
		return fmt.Errorf("failed to stage file '%s': %w", filePath, err)
	}
	log.Printf("Written and staged file: %s", filePath)
	return nil
}

func (s *GitService) Commit(message string) (plumbing.Hash, error) {
	worktree, err := s.repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to get worktree: %w", err)
	}
	status, err := worktree.Status()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to get worktree status: %w", err)
	}
	if status.IsClean() {
		log.Println("Worktree is clean, nothing to commit.")
		headRef, errHead := s.repo.Head()
		if errHead != nil {
			if errors.Is(errHead, plumbing.ErrReferenceNotFound) {
				return plumbing.ZeroHash, nil
			}
			return plumbing.ZeroHash, fmt.Errorf("failed to get HEAD ref on clean worktree: %w", errHead)
		}
		return headRef.Hash(), nil
	}
	commit, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "AI Agent",
			Email: "ai@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to commit changes: %w", err)
	}
	log.Printf("Committed changes with hash: %s", commit.String())
	return commit, nil
}

func (s *GitService) CreateBranch(branchName string) error {
	headRef, err := s.repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return fmt.Errorf("cannot create branch '%s': HEAD reference not found (no commits yet?)", branchName)
		}
		return fmt.Errorf("failed to get HEAD ref: %w", err)
	}
	refName := plumbing.NewBranchReferenceName(branchName)
	_, errCheck := s.repo.Reference(refName, false)
	if errCheck == nil {
		return fmt.Errorf("branch '%s' already exists", branchName)
	} else if !errors.Is(errCheck, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("failed to check if branch '%s' exists: %w", branchName, errCheck)
	}
	ref := plumbing.NewHashReference(refName, headRef.Hash())
	err = s.repo.Storer.SetReference(ref)
	if err != nil {
		return fmt.Errorf("failed to create branch '%s': %w", branchName, err)
	}
	log.Printf("Created new branch: %s pointing to %s", branchName, headRef.Hash().String())
	return nil
}

func (s *GitService) RepoHeadHash() (plumbing.Hash, error) {
	headRef, err := s.repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return plumbing.ZeroHash, nil
		}
		return plumbing.ZeroHash, fmt.Errorf("failed to get HEAD ref: %w", err)
	}
	return headRef.Hash(), nil
}

func (s *GitService) PushBranch(branchName string) error {
	log.Printf("Attempting to push branch '%s' to remote origin", branchName)

	if s.username == "" || s.password == "" {
		log.Println("Skipping push: Git username or PAT/password not configured in GitService.")
		return nil
	}

	localRef := plumbing.NewBranchReferenceName(branchName)
	remoteRef := plumbing.NewBranchReferenceName(branchName)
	refSpec := config.RefSpec(fmt.Sprintf("%s:%s", localRef, remoteRef))

	err := refSpec.Validate()
	if err != nil {
		return fmt.Errorf("invalid refspec created for branch '%s': %w", branchName, err)
	}

	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth: &http.BasicAuth{
			Username: s.username,
			Password: s.password,
		},
		Progress: os.Stdout,
	}

	log.Printf(
		"Pushing with options: Remote=%s, RefSpec=%s, Auth=BasicAuth(User:%s)",
		pushOpts.RemoteName,
		pushOpts.RefSpecs[0],
		pushOpts.Auth.(*http.BasicAuth).Username,
	)

	err = s.repo.Push(pushOpts)
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			log.Printf("Branch '%s' is already up-to-date on remote origin.", branchName)
			return nil
		}
		if strings.Contains(err.Error(), "authentication required") || strings.Contains(err.Error(), "authorization failed") {
			log.Printf("Push failed due to potential authentication error for branch '%s'. Check PAT permissions. Error: %v", branchName, err)
			return fmt.Errorf("pushing branch '%s' failed: authentication required - check PAT permissions: %w", branchName, err)
		}
		log.Printf("Failed to push branch '%s': %v", branchName, err)
		return fmt.Errorf("failed to push branch '%s' to remote: %w", branchName, err)
	}

	log.Printf("Successfully pushed branch '%s' to remote origin.", branchName)
	return nil
}