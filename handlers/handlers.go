// handlers/handlers.go
package handlers

import (
  "context"
  "database/sql"
  "fmt"
  "html"
  "log"
  "net/http"
  "strings"
  "time"

  "go.temporal.io/sdk/client"
  // Use an alias 't' for the temporal package to avoid naming conflicts if any,
  // or just use the full path if preferred. Using 't' for brevity here.
  t "hammer/temporal"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
  TemporalClient client.Client
  DB             *sql.DB
}

// NewHandler creates a new Handler instance
func NewHandler(tc client.Client, db *sql.DB) *Handler {
  return &Handler{
    TemporalClient: tc,
    DB:             db,
  }
}

// RootHandler serves the main HTML page
func (h *Handler) RootHandler(w http.ResponseWriter, r *http.Request) {
  if r.URL.Path != "/" {
    http.NotFound(w, r)
    return
  }
  // Consider embedding static files if preferred later
  http.ServeFile(w, r, "static/index.html")
}

// SubmitHandler handles prompt submission and starts the Temporal workflow
func (h *Handler) SubmitHandler(w http.ResponseWriter, r *http.Request) {
  if r.Method != http.MethodPost {
    http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
    return
  }
  if err := r.ParseForm(); err != nil {
    log.Printf("Error parsing form: %v", err)
    http.Error(w, "Error parsing form", http.StatusBadRequest)
    return
  }
  prompt := r.FormValue("prompt")
  if prompt == "" {
    http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
    return
  }

  log.Printf("Received prompt via HTTP: %s", prompt)

  workflowOptions := client.StartWorkflowOptions{
    ID:        fmt.Sprintf("code-gen-%d", time.Now().UnixNano()),
    TaskQueue: t.TaskQueue, // Use TaskQueue from temporal package
    // Add other options like timeouts if needed
  }

  // Execute the workflow using the correct workflow function reference
  we, err := h.TemporalClient.ExecuteWorkflow(context.Background(), workflowOptions, t.CodeGenerationWorkflow, prompt)
  if err != nil {
    log.Printf("Error starting workflow: %v", err)
    http.Error(w, "Failed to start generation task", http.StatusInternalServerError)
    return
  }

  log.Printf("Started workflow | WorkflowID: %s | RunID: %s", we.GetID(), we.GetRunID())

  // Respond with the initial HTML snippet that triggers polling
  workflowID := we.GetID()
  responseHTML := fmt.Sprintf(`
        <div id="polling-area-%s"
             hx-get="/get-result/%s"
             hx-trigger="load delay:2s"
             hx-swap="outerHTML">
             <p>Processing... (ID: %s)</p>
        </div>`,
    html.EscapeString(workflowID), // Use ID in polling area ID
    html.EscapeString(workflowID), // Use ID in URL
    html.EscapeString(workflowID), // Display ID
  )

  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  fmt.Fprint(w, responseHTML)
}

// GetResultHandler queries SQLite and returns status/result or continues polling
func (h *Handler) GetResultHandler(w http.ResponseWriter, r *http.Request) {
  if r.Method != http.MethodGet {
    http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
    return
  }

  // Extract workflowID from URL path, e.g., /get-result/workflow-xyz
  // Ensure robust parsing, especially if deployed behind proxies
  path := strings.TrimPrefix(r.URL.Path, "/get-result/")
  workflowID := strings.TrimSuffix(path, "/") // Handle optional trailing slash
  if workflowID == "" {
    http.Error(w, "Missing or invalid workflow ID", http.StatusBadRequest)
    return
  }

  // Query the database
  var finalResult sql.NullString // Use sql.NullString for potentially NULL results
  var status string
  var errorDetails sql.NullString

  query := `SELECT final_result, status, error_details FROM results WHERE workflow_id = ?`
  // Use DB from the handler struct 'h'
  err := h.DB.QueryRowContext(r.Context(), query, workflowID).Scan(&finalResult, &status, &errorDetails)

  var responseHTML string

  if err != nil {
    if err == sql.ErrNoRows {
      // Workflow likely hasn't saved anything yet, or ID is invalid after completion?
      // Keep polling by returning the same polling div structure
      log.Printf("getResultHandler: No result found yet for WorkflowID %s\n", workflowID)
      responseHTML = fmt.Sprintf(`
                <div id="polling-area-%s"
                     hx-get="/get-result/%s"
                     hx-trigger="load delay:3s"
                     hx-swap="outerHTML">
                     <p>Checking status... (ID: %s)</p>
                </div>`,
        html.EscapeString(workflowID), // Use ID in polling area ID
        html.EscapeString(workflowID), // Use ID in URL
        html.EscapeString(workflowID), // Display ID
      )
    } else {
      // Actual database error
      log.Printf("getResultHandler: Database query error for WorkflowID %s: %v\n", workflowID, err)
      // Stop polling and show error
      responseHTML = fmt.Sprintf("<p>Error retrieving result: %s</p>", html.EscapeString(err.Error()))
    }
  } else {
    // Row found, check status
    log.Printf("getResultHandler: Found record for WorkflowID %s with Status %s\n", workflowID, status)
    switch status {
    case "COMPLETED":
      // Stop polling, display result
      resultText := "No result content."
      if finalResult.Valid {
        resultText = finalResult.String
      }
      // Use <pre> for code formatting, escape the result
      responseHTML = fmt.Sprintf("<p><strong>Status: %s</strong></p><pre><code>%s</code></pre>",
        html.EscapeString(status),
        html.EscapeString(resultText))
    case "FAILED":
      // Stop polling, display error
      errorText := "An unknown error occurred."
      if errorDetails.Valid && errorDetails.String != "" {
        errorText = errorDetails.String
      }
      responseHTML = fmt.Sprintf("<p><strong>Status: %s</strong></p><p>Error: %s</p>",
        html.EscapeString(status),
        html.EscapeString(errorText))
    case "PENDING":
      fallthrough // Treat PENDING same as UNKNOWN/other intermediate status
    default: // Includes PENDING or any other intermediate status
      // Keep polling, display current status
      responseHTML = fmt.Sprintf(`
                <div id="polling-area-%s"
                     hx-get="/get-result/%s"
                     hx-trigger="load delay:3s"
                     hx-swap="outerHTML">
                     <p>Processing [%s]... (ID: %s)</p>
                </div>`,
        html.EscapeString(workflowID), // Use ID in polling area ID
        html.EscapeString(workflowID), // Use ID in URL
        html.EscapeString(status),     // Display current status
        html.EscapeString(workflowID), // Display ID
      )
    }
  }

  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  fmt.Fprint(w, responseHTML)
}
