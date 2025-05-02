// main.go
package main

import (
  "log"
  "net/http"
  "os"
  "os/signal"
  "syscall"

  "github.com/joho/godotenv"

  "hammer/db"
  "hammer/handlers"
  "hammer/temporal"
)

const port = ":8080"

func main() {
  err := godotenv.Load()
  if err != nil {
    log.Println("Info: .env file not found or error loading, using system environment variables.")
  } else {
    log.Println("Loaded environment variables from .env file.")
  }

  log.Println("Starting Hammer application...")

  // Initialize Database
  sqlDB, err := db.InitDB()
  if err != nil {
    log.Fatalf("Failed to initialize database: %v", err)
  }
  defer func() {
    if err := sqlDB.Close(); err != nil {
        log.Printf("Error closing database: %v", err)
    } else {
        log.Println("Database connection closed.")
    }
  }()


  // Initialize Temporal Client
  temporalClient, err := temporal.NewClient()
  if err != nil {
    log.Fatalf("Failed to create Temporal client: %v", err)
  }
  defer func() {
      temporalClient.Close()
      log.Println("Temporal client connection closed.")
  }()


  // Start Temporal Worker in a goroutine
  // Pass the DB connection needed by activities
  workerErrChan := make(chan error, 1) // Channel to receive error from worker goroutine
  go func() {
     workerErrChan <- temporal.StartWorker(temporalClient, sqlDB)
  }()


  // Initialize HTTP Handlers with dependencies
  httpHandlers := handlers.NewHandler(temporalClient, sqlDB)

  // Setup HTTP Server Routes
  mux := http.NewServeMux() // Use NewServeMux for clarity
  mux.HandleFunc("/", httpHandlers.RootHandler)
  mux.HandleFunc("/submit-prompt", httpHandlers.SubmitHandler)
  mux.HandleFunc("/get-result/", httpHandlers.GetResultHandler) // Note trailing slash

  // Start HTTP server
  log.Printf("Starting HTTP server on http://localhost%s\n", port)
  server := &http.Server{Addr: port, Handler: mux}
  go func() {
    if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
      log.Fatalf("HTTP server failed: %v", err)
    }
  }()

  // Wait for termination signal or worker error
  log.Println("Application started successfully. Press Ctrl+C to exit.")
  sigChan := make(chan os.Signal, 1)
  signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

  select {
  case err := <-workerErrChan:
      log.Printf("Temporal worker exited with error: %v", err)
  case sig := <-sigChan:
      log.Printf("Received signal %v, shutting down...", sig)
  }

  // Graceful shutdown (optional for HTTP server, worker shutdown handled by its context)
  // ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  // defer cancel()
  // if err := server.Shutdown(ctx); err != nil {
  //    log.Printf("HTTP server shutdown error: %v", err)
  //}

  log.Println("Application shut down.")
}
