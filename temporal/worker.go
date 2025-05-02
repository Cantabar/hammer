// temporal/worker.go
package temporal

import (
  "database/sql"
  "log"

  "go.temporal.io/sdk/client"
  "go.temporal.io/sdk/worker"
)

// StartWorker initializes and runs a Temporal worker for Phase 4
func StartWorker(c client.Client, db *sql.DB) error {
  // Assign DB connection to the package-level variable for local activities
  if db == nil {
    log.Fatal("FATAL: DB connection passed to StartWorker is nil")
  }
  localActivitiesDB = db
  log.Println("Database connection assigned for use by local activities.")

  // Create activity struct instance WITH dependencies for STANDARD activities
  // This instance holds the DB connection, even if agent activities don't use it directly now
  activities := &Activities{DB: db}

  // Create worker that listens on the defined task queue
  workerOptions := worker.Options{
    // Configure worker options here if needed (e.g., MaxConcurrentActivityExecutionSize)
  }
  w := worker.New(c, TaskQueue, workerOptions) // Use TaskQueue from shared.go

  // Register Workflow definition
  w.RegisterWorkflow(CodeGenerationWorkflow)

  // Register STANDARD (Agent) activities using the struct instance methods
  w.RegisterActivity(activities.PlanningAgentActivity)
  w.RegisterActivity(activities.ExecutionAgentActivity)
  w.RegisterActivity(activities.CombineAgentActivity)

  // NOTE: Local activities (SaveResultActivity, CreatePendingRecordActivity) are
  // registered implicitly by being called via workflow.ExecuteLocalActivity
  // and using the package-level 'localActivitiesDB'.
  // DO NOT call worker.RegisterLocalActivity.

  // Start the worker. This call blocks until the worker is stopped or an error occurs.
  log.Printf("Starting Temporal worker on task queue '%s'...", TaskQueue)
  err := w.Run(worker.InterruptCh()) // Listens on InterruptCh for shutdown signals
  if err != nil {
    log.Printf("Temporal worker Run() failed: %v", err)
    return err // Return error to main goroutine
  }

  // This log message executes only after the worker stops gracefully
  log.Println("Temporal worker stopped gracefully.")
  return nil
}
