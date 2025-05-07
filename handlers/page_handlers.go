package handlers

import (
  "fmt"
  "html/template"
  "log"
  "net/http"
  "os"
  "time"

  "hammer/workflows"
  "hammer/shared" // Adjust 'project_name'
  "github.com/go-chi/chi/v5"
  "go.temporal.io/sdk/client"
  temporalApiEnums "go.temporal.io/api/enums/v1"
)

type PageHandler struct {
  TemporalClient client.Client
  Template       *template.Template
  TaskQueue      string
  RepoURL        string
  BranchPrefix   string
}

func NewPageHandler(client client.Client) (*PageHandler, error) {
  tmpl, err := template.ParseFiles("templates/index.html.tmpl")
  if err != nil {
    return nil, fmt.Errorf("failed to parse template: %w", err)
  }

  // Get config from environment
  taskQueue := os.Getenv("TEMPORAL_TASK_QUEUE")
  if taskQueue == "" {
    taskQueue = "code-gen-queue" // Default if not set
  }
   repoURL := os.Getenv("TARGET_REPO_URL")
    if repoURL == "" {
        return nil, fmt.Errorf("TARGET_REPO_URL environment variable not set")
    }
    branchPrefix := os.Getenv("BRANCH_PREFIX") // Optional, workflow uses default if empty


  return &PageHandler{
    TemporalClient: client,
    Template:       tmpl,
    TaskQueue:      taskQueue,
    RepoURL:        repoURL,
    BranchPrefix:   branchPrefix, // Store prefix if needed elsewhere
  }, nil
}

func (h *PageHandler) RegisterRoutes(r *chi.Mux) {
  r.Get("/", h.HandleIndex)
  r.Post("/submit", h.HandleSubmit)
    // Add a route to check workflow status (optional but useful)
    r.Get("/status/{workflowID}", h.HandleStatus)
}

// HandleIndex serves the main page.
func (h *PageHandler) HandleIndex(w http.ResponseWriter, r *http.Request) {
  err := h.Template.Execute(w, nil)
  if err != nil {
    log.Printf("Error executing template: %v", err)
    http.Error(w, "Internal Server Error", http.StatusInternalServerError)
  }
}

// HandleSubmit receives the prompt and starts the Temporal workflow.
func (h *PageHandler) HandleSubmit(w http.ResponseWriter, r *http.Request) {
  if err := r.ParseForm(); err != nil {
    log.Printf("Error parsing form: %v", err)
    http.Error(w, "Bad Request", http.StatusBadRequest)
    return
  }

  prompt := r.FormValue("prompt")
  if prompt == "" {
    http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
    return
  }

  // Start Workflow
  options := client.StartWorkflowOptions{
    ID:        fmt.Sprintf("codegen-%d", time.Now().UnixNano()), // Unique workflow ID
    TaskQueue: h.TaskQueue,
    // Potentially set WorkflowExecutionTimeout, WorkflowRunTimeout
  }

  wfInput := shared.WorkflowInput{
    UserPrompt: prompt,
    RepoURL:    h.RepoURL,
    // BranchPrefix is read from env within the workflow now
  }

  log.Printf("Starting workflow %s for prompt: %s", options.ID, prompt)
  wfRun, err := h.TemporalClient.ExecuteWorkflow(
    r.Context(),
    options,
    workflows.CodeGenWorkflow, // Workflow function reference
    wfInput,
  )

  if err != nil {
    log.Printf("Error starting workflow: %v", err)
    http.Error(w, "Failed to start generation task", http.StatusInternalServerError)
    return
  }

  log.Printf("Workflow started successfully: ID=%s, RunID=%s", wfRun.GetID(), wfRun.GetRunID())

  // Respond with HTMX snippet indicating success and providing workflow ID
  // Include HX-Trigger header for polling if desired
  statusURL := fmt.Sprintf("/status/%s", wfRun.GetID())
   w.Header().Set("HX-Trigger", fmt.Sprintf(`{"pollStatus": {"url": "%s", "interval": "3s"}}`, statusURL)) // Trigger polling
   fmt.Fprintf(w, `<div class="processing" id="workflow-%s">
                      Task submitted. Workflow ID: %s (RunID: %s). Checking status...
                      <div hx-get="%s" hx-trigger="load, pollStatus from:body" hx-swap="outerHTML"></div>
                   </div>`,
                   wfRun.GetID(), wfRun.GetID(), wfRun.GetRunID(), statusURL)

}


// HandleStatus checks the status of a workflow and returns an HTMX snippet.
func (h *PageHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
    workflowID := chi.URLParam(r, "workflowID")
    // RunID is usually empty for DescribeWorkflowExecution, ID is sufficient
    log.Printf("Checking status for workflow: %s", workflowID)

    resp, err := h.TemporalClient.DescribeWorkflowExecution(r.Context(), workflowID, "")
    if err != nil {
        log.Printf("Error describing workflow %s: %v", workflowID, err)
        // Don't stop polling on transient errors maybe? Or signal stop?
        // For now, return an error message but allow polling to potentially continue
         fmt.Fprintf(w, `<div id="workflow-%s" class="error">Error checking status for %s: %v</div>`, workflowID, workflowID, err)
        return
    }

    status := resp.GetWorkflowExecutionInfo().GetStatus()
    resultDivID := fmt.Sprintf("workflow-%s", workflowID) // ID for the whole status div

    switch status {
    case temporalApiEnums.WORKFLOW_EXECUTION_STATUS_RUNNING:
         // Still running, keep polling indicator
         w.Header().Set("HX-Trigger", fmt.Sprintf(`{"pollStatus": {"url": "/status/%s", "interval": "3s"}}`, workflowID))
         fmt.Fprintf(w, `<div id="%s" class="processing">Workflow %s is running... Status: %s</div>`, resultDivID, workflowID, status.String())
    case temporalApiEnums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
         // Workflow finished, get result and stop polling
         var result shared.WorkflowOutput
         // Need the RunID to get the result of a *specific* run
         runID := resp.GetWorkflowExecutionInfo().GetExecution().GetRunId()
         err := h.TemporalClient.GetWorkflow(r.Context(), workflowID, runID).Get(r.Context(), &result)
         if err != nil {
              log.Printf("Error getting workflow result for %s (%s): %v", workflowID, runID, err)
             fmt.Fprintf(w, `<div id="%s" class="error">Workflow %s completed, but failed to get result: %v</div>`, resultDivID, workflowID, err)
         } else {
              log.Printf("Workflow %s completed successfully. Branch: %s", workflowID, result.BranchName)
              fmt.Fprintf(w, `<div id="%s" class="success">Workflow %s completed! ✅<br/>Result: %s</div>`, resultDivID, workflowID, template.HTMLEscapeString(result.Message))
         }
    case temporalApiEnums.WORKFLOW_EXECUTION_STATUS_FAILED, temporalApiEnums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, temporalApiEnums.WORKFLOW_EXECUTION_STATUS_TERMINATED, temporalApiEnums.WORKFLOW_EXECUTION_STATUS_CANCELED:
         // Workflow ended unsuccessfully, stop polling
          // Attempt to get error details if failed
          var workflowErr error
          runID := resp.GetWorkflowExecutionInfo().GetExecution().GetRunId()
          err := h.TemporalClient.GetWorkflow(r.Context(), workflowID, runID).Get(r.Context(), nil) // Getting result into nil extracts the error
          if err != nil {
              workflowErr = err
          }
          log.Printf("Workflow %s ended with status %s. Error: %v", workflowID, status.String(), workflowErr)
          fmt.Fprintf(w, `<div id="%s" class="error">Workflow %s ended with status: %s ❌<br/>Error: %v</div>`, resultDivID, workflowID, status.String(), workflowErr)
    default:
        // Unknown status, keep polling?
         w.Header().Set("HX-Trigger", fmt.Sprintf(`{"pollStatus": {"url": "/status/%s", "interval": "5s"}}`, workflowID)) // Poll less frequently
         fmt.Fprintf(w, `<div id="%s" class="processing">Workflow %s has status: %s. Continuing check...</div>`, resultDivID, workflowID, status.String())
    }
}
