package main

import (
  "log"
  "net/http"
  "os"
  "time"

  "hammer/activities"
  "hammer/handlers"
  "hammer/services"
  "hammer/workflows"

  "github.com/go-chi/chi/v5"
  "github.com/go-chi/chi/v5/middleware"
  "github.com/joho/godotenv"
  "go.temporal.io/sdk/client"
  "go.temporal.io/sdk/worker"
  "go.temporal.io/sdk/activity"
)

func main() {
	// Load Env Vars (.env takes precedence over system env)
	 err := godotenv.Load()
	 if err != nil {
		 log.Println("No .env file found, using environment variables or defaults")
	 }

	 // Read necessary config (Temporal, OpenAI key)
	 temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	 if temporalAddr == "" { temporalAddr = "localhost:7233" }
	 apiKey := os.Getenv("OPENAI_API_KEY")
	 if apiKey == "" { log.Fatalln("OPENAI_API_KEY environment variable not set") }
	 // Git creds are now read within the workflow via workflow.Getenv
	 // gitUsername := os.Getenv("GIT_USERNAME")
	 // gitPat := os.Getenv("GIT_PAT")
	 // log.Printf("Git Username Loaded: %t", gitUsername != "") // Check if loaded


	// Init Temporal Client
	 temporalClient, err := client.Dial(client.Options{ HostPort: temporalAddr, Logger:   NewTemporalLogger(log.New(os.Stdout, "TEMPORAL_CLIENT: ", log.LstdFlags)), })
	 if err != nil { log.Fatalf("Unable to create Temporal client: %v", err) }
	 defer temporalClient.Close()


	// Init Services (LLM Service needed by activities)
	 llmService := services.NewLLMService(apiKey)


	// Init Temporal Worker
	 taskQueue := os.Getenv("TEMPORAL_TASK_QUEUE")
	 if taskQueue == "" { taskQueue = "code-gen-queue" }
	 w := worker.New(temporalClient, taskQueue, worker.Options{})

	// Register Workflows
	 w.RegisterWorkflow(workflows.CodeGenWorkflow)

	// Register Activities
	 llmActivities := activities.NewLLMActivities(llmService)
	 gitActivities := activities.NewGitActivities() // Holds state map

	 // LLM Activities
	 w.RegisterActivityWithOptions(llmActivities.PlanStepsActivity, activity.RegisterOptions{Name: activities.ActivityName_PlanSteps})
	 w.RegisterActivityWithOptions(llmActivities.EvaluateFilesActivity, activity.RegisterOptions{Name: activities.ActivityName_EvaluateFiles})
	 w.RegisterActivityWithOptions(llmActivities.GenerateCodeActivity, activity.RegisterOptions{Name: activities.ActivityName_GenerateCode})

	 // Git Activities
	 w.RegisterActivityWithOptions(gitActivities.InitGitActivity, activity.RegisterOptions{Name: activities.ActivityName_InitGit})
	 w.RegisterActivityWithOptions(gitActivities.CleanupGitActivity, activity.RegisterOptions{Name: activities.ActivityName_CleanupGit})
	 w.RegisterActivityWithOptions(gitActivities.ListFilesGitActivity, activity.RegisterOptions{Name: activities.ActivityName_ListFilesGit})
	 w.RegisterActivityWithOptions(gitActivities.ReadFilesGitActivity, activity.RegisterOptions{Name: activities.ActivityName_ReadFilesGit})
	 w.RegisterActivityWithOptions(gitActivities.WriteFilesAndCommitActivity, activity.RegisterOptions{Name: activities.ActivityName_WriteFilesAndCommit})
	 w.RegisterActivityWithOptions(gitActivities.CreateBranchActivity, activity.RegisterOptions{Name: activities.ActivityName_CreateBranch})
	 w.RegisterActivityWithOptions(gitActivities.PushBranchActivity, activity.RegisterOptions{Name: activities.ActivityName_PushBranch})

	// Start Worker
	 err = w.Start()
	 if err != nil { log.Fatalf("Unable to start Temporal worker: %v", err) }
	 defer w.Stop()


	// Init Router and Handlers
	 r := chi.NewRouter()
	 r.Use(middleware.Logger, middleware.Recoverer, middleware.Timeout(60*time.Second))
	 pageHandler, err := handlers.NewPageHandler(temporalClient)
	 if err != nil { log.Fatalf("Failed to create page handler: %v", err) }
	 pageHandler.RegisterRoutes(r)


	// Start HTTP Server
	 port := os.Getenv("APP_PORT")
	 if port == "" { port = "3000" }
	 serverAddr := ":" + port
	 log.Printf("Starting HTTP server on %s", serverAddr)
	 if err := http.ListenAndServe(serverAddr, r); err != nil { log.Fatalf("HTTP server failed: %v", err) }
}


// --- Temporal Logger Adapter ---
// Wraps Go's standard logger for Temporal SDK compatibility.

type TemporalLogger struct {
    logger *log.Logger
}

func NewTemporalLogger(l *log.Logger) *TemporalLogger {
    return &TemporalLogger{logger: l}
}

func (l *TemporalLogger) Debug(msg string, keyvals ...interface{}) {
    l.logger.Printf("DEBUG: %s %v\n", msg, keyvals)
}

func (l *TemporalLogger) Info(msg string, keyvals ...interface{}) {
    l.logger.Printf("INFO: %s %v\n", msg, keyvals)
}

func (l *TemporalLogger) Warn(msg string, keyvals ...interface{}) {
    l.logger.Printf("WARN: %s %v\n", msg, keyvals)
}

func (l *TemporalLogger) Error(msg string, keyvals ...interface{}) {
    l.logger.Printf("ERROR: %s %v\n", msg, keyvals)
}
