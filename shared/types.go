// shared/types.go
package shared

// WorkflowInput defines the input for the code generation workflow.
type WorkflowInput struct {
  UserPrompt string
  RepoURL    string // URL of the repo to clone
}

// WorkflowOutput defines the result of the workflow.
type WorkflowOutput struct {
  BranchName string
  Message    string
}

// GenerateCodeActivityInput defines input for the code generation activity.
type GenerateCodeActivityInput struct {
  StepDescription      string
  RelevantFilesContent map[string]string // map[filePath]content
  OriginalUserPrompt   string            // Pass original prompt for context
}

// GenerateCodeActivityResult defines the output of the code generation activity.
type GenerateCodeActivityResult struct {
  GeneratedFiles map[string]string // map[filePath]newContent
}

// EvaluateFilesActivityInput defines input for the file evaluation activity.
type EvaluateFilesActivityInput struct {
  StepDescription string
  AllFiles        []string // List of all files currently in the repo
}

// EvaluateFilesActivityResult defines the output of the file evaluation activity.
type EvaluateFilesActivityResult struct {
  RelevantFiles []string
}

// --- Input structs for stateful Git activities ---
// (These might reference other shared types if needed)

type GitCredentials struct {
   Username string
   Password string
}

type InitGitActivityInput struct {
  WorkflowID  string
  RepoURL     string
  Credentials GitCredentials
}
type CleanupGitActivityInput struct {
  WorkflowID string
}
type ListFilesGitActivityInput struct {
  WorkflowID string
}
type ReadFilesGitActivityInput struct {
  WorkflowID string
  FilePaths  []string
}
type WriteAndCommitInput struct {
  WorkflowID    string
  Changes       map[string]string // file -> content
  CommitMessage string
}
type CreateBranchInput struct {
  WorkflowID string
  BranchName string
}
type PushBranchActivityInput struct {
  WorkflowID  string
  BranchName  string
}
