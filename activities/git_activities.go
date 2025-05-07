package activities

import (
  "context"
  "fmt"
  "log"

  "hammer/services"
  "hammer/shared"
)

const (
  ActivityName_InitGit              = "InitGitActivity"
  ActivityName_CleanupGit           = "CleanupGitActivity"
  ActivityName_ListFilesGit         = "ListFilesGitActivity"
  ActivityName_ReadFilesGit         = "ReadFilesGitActivity"
  ActivityName_WriteFilesAndCommit  = "WriteFilesAndCommitActivity"
  ActivityName_CreateBranch         = "CreateBranchActivity"
  ActivityName_PushBranch           = "PushBranchActivity"
)

type GitActivities struct {
  gitServiceMap map[string]*services.GitService
}

// ApplyChangesActivityInput - defines how changes are passed
type ApplyChangesActivityInput struct {
   // RepoState // How to represent repo state efficiently? Maybe commit hash before changes?
   Changes map[string]string // filePath -> newContent
   CommitMessage string
}

// ApplyChangesActivityResult - result of applying changes
type ApplyChangesActivityResult struct {
   NewCommitHash string
   // NewRepoState // Representation of state after commit
}

type StatefulGitActivityInput struct {
    WorkflowID string // Need to identify which GitService instance to use
    // ... other specific args for the operation
}
type WriteAndCommitInput struct {
    WorkflowID string
    Changes map[string]string // file -> content
    CommitMessage string
}
type CreateBranchInput struct {
    WorkflowID string
    BranchName string
}

func (ga *GitActivities) RegisterGitServiceForWorkflow(workflowID string, service *services.GitService) {
  log.Printf("Registering GitService for workflow %s", workflowID)
  ga.gitServiceMap[workflowID] = service
}
func (ga *GitActivities) CleanupGitServiceForWorkflow(workflowID string) {
  log.Printf("Cleaning up GitService for workflow %s", workflowID)
  delete(ga.gitServiceMap, workflowID)
}
func (ga *GitActivities) getServiceForWorkflow(workflowID string) (*services.GitService, error) {
  service, ok := ga.gitServiceMap[workflowID]
  if !ok {
    // Attempt to re-register if lost? Unlikely safe.
    return nil, fmt.Errorf("no GitService found for workflow ID %s in activity worker map", workflowID)
  }
  return service, nil
}

func (a *GitActivities) InitGitActivity(ctx context.Context, input shared.InitGitActivityInput) error {
  log.Printf("Attempting to initialize GitService for workflow %s", input.WorkflowID)
  if _, exists := a.gitServiceMap[input.WorkflowID]; exists {
    log.Printf("Warning: GitService already exists for workflow %s. Re-initializing.", input.WorkflowID)
  }
  gitService, err := services.NewGitService(input.RepoURL, input.Credentials)
  if err != nil {
    log.Printf("Error initializing GitService for workflow %s: %v", input.WorkflowID, err)
    return err
  }
  a.RegisterGitServiceForWorkflow(input.WorkflowID, gitService)
  log.Printf("Successfully initialized GitService for workflow %s", input.WorkflowID)
  return nil
}

func (a *GitActivities) CleanupGitActivity(ctx context.Context, input shared.CleanupGitActivityInput) error {
    log.Printf("Attempting cleanup for workflow %s", input.WorkflowID)
    a.CleanupGitServiceForWorkflow(input.WorkflowID) // CleanupGitServiceForWorkflow should be idempotent
    return nil
}

func (a *GitActivities) ListFilesGitActivity(ctx context.Context, input shared.ListFilesGitActivityInput) ([]string, error) {
    gitService, err := a.getServiceForWorkflow(input.WorkflowID)
    if err != nil {
        return nil, err
    }
    return gitService.ListFiles()
}

func (a *GitActivities) ReadFilesGitActivity(ctx context.Context, input shared.ReadFilesGitActivityInput) (map[string]string, error) {
    gitService, err := a.getServiceForWorkflow(input.WorkflowID)
     if err != nil { return nil, err }
     contents := make(map[string]string)
     for _, p := range input.FilePaths {
         content, err := gitService.ReadFile(p)
         if err != nil {
             log.Printf("Warning: ReadFilesGitActivity could not read '%s' for workflow %s: %v", p, input.WorkflowID, err)
             // Skip file on error
             continue
         }
         contents[p] = content
     }
     return contents, nil
}

func (a *GitActivities) PushBranchActivity(ctx context.Context, input shared.PushBranchActivityInput) error {
  gitService, err := a.getServiceForWorkflow(input.WorkflowID)
  if err != nil {
    return fmt.Errorf("failed to get git service for push activity (workflow %s): %w", input.WorkflowID, err)
  }

  err = gitService.PushBranch(input.BranchName)
  if err != nil {
    // Error is already logged in PushBranch, just bubble it up
    return fmt.Errorf("push branch activity failed for workflow %s, branch %s: %w", input.WorkflowID, input.BranchName, err)
  }

  log.Printf("PushBranchActivity completed successfully for workflow %s, branch %s", input.WorkflowID, input.BranchName)
  return nil
}

func NewGitActivities() *GitActivities {
  return &GitActivities{
    gitServiceMap: make(map[string]*services.GitService),
  }
}

// Helper to get the service for the current workflow run
func (ga *GitActivities) getService(ctx context.Context) (*services.GitService, error) {
  return nil, fmt.Errorf("cannot get GitService: WorkflowID mechanism not implemented in this simplified example")
}

func (a *GitActivities) ListFilesActivity(ctx context.Context, input shared.ReadFilesGitActivityInput) ([]string, error) {
  gitService, err := a.getServiceForWorkflow(input.WorkflowID)
  if err != nil { return nil, err }
  return gitService.ListFiles()
}

func (a *GitActivities) ReadFilesActivity(ctx context.Context, input shared.ReadFilesGitActivityInput) (map[string]string, error) {
  gitService, err := a.getServiceForWorkflow(input.WorkflowID)
  if err != nil { return nil, err }
  contents := make(map[string]string)
  for _, p := range input.FilePaths {
    content, err := gitService.ReadFile(p)
    if err != nil { log.Printf("Warning: ReadFilesGitActivity could not read '%s' for workflow %s: %v", p, input.WorkflowID, err); continue }
    contents[p] = content
  }
  return contents, nil
}

func (a *GitActivities) WriteFilesAndCommitActivity(ctx context.Context, input WriteAndCommitInput) (string, error) {
    gitService, err := a.getServiceForWorkflow(input.WorkflowID)
    if err != nil {
        return "", err
    }

    if len(input.Changes) == 0 {
        log.Printf("No changes to write for workflow %s", input.WorkflowID)
        // Get current commit hash if needed, or return empty
        headRef, err := gitService.RepoHeadHash() // Assumes RepoHeadHash() exists in service
         if err != nil {
             log.Printf("Warning: Could not get HEAD hash for no-op commit: %v", err)
             return "", nil // Or return specific indicator
         }
         return headRef.String(), nil
    }


    for filePath, content := range input.Changes {
        err := gitService.WriteFile(filePath, content) // WriteFile now also stages
        if err != nil {
            return "", fmt.Errorf("failed to write/stage file '%s' for workflow %s: %w", filePath, input.WorkflowID, err)
        }
    }

    commitHash, err := gitService.Commit(input.CommitMessage)
    if err != nil {
         return "", fmt.Errorf("failed to commit changes for workflow %s: %w", input.WorkflowID, err)
    }

    return commitHash.String(), nil
}


func (a *GitActivities) CreateBranchActivity(ctx context.Context, input CreateBranchInput) error {
    gitService, err := a.getServiceForWorkflow(input.WorkflowID)
    if err != nil {
        return err
    }

    err = gitService.CreateBranch(input.BranchName)
    if err != nil {
         return fmt.Errorf("failed to create branch '%s' for workflow %s: %w", input.BranchName, input.WorkflowID, err)
    }
    return nil
}
