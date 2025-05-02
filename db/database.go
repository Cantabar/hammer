// db/database.go
package db

import (
  "database/sql"
  "fmt"
  "log"
  "os" // Import os if checking file existence or other file ops needed

  _ "github.com/mattn/go-sqlite3" // SQLite driver
)

const dbName = "./hammer.db"

// InitDB initializes the database connection and creates tables if needed
func InitDB() (*sql.DB, error) {
  log.Println("Initializing SQLite database:", dbName)

  // Check if DB file exists (optional, helps logging)
  _, err := os.Stat(dbName)
  dbExists := !os.IsNotExist(err)
  if !dbExists {
      log.Printf("Database file '%s' not found, will be created.", dbName)
  }

  d, err := sql.Open("sqlite3", dbName+"?_journal_mode=WAL") // Use WAL mode for better concurrency
  if err != nil {
    return nil, fmt.Errorf("failed to open database: %w", err)
  }

  // Ping DB to ensure connection is live
  if err = d.Ping(); err != nil {
     d.Close()
     return nil, fmt.Errorf("failed to connect to database: %w", err)
  }

  // Enable foreign key support if needed (good practice)
  _, err = d.Exec("PRAGMA foreign_keys = ON;")
  if err != nil {
      d.Close()
      return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
  }


  log.Println("Database connection established. Ensuring table structure...")

  // Create results table if it doesn't exist 
  createTableSQL := `
  CREATE TABLE IF NOT EXISTS results (
      workflow_id TEXT PRIMARY KEY,
      prompt TEXT,
      plan TEXT, 
      step_outputs TEXT, -- Store as JSON string
      final_result TEXT,
      status TEXT NOT NULL DEFAULT 'UNKNOWN', -- Ensure status is never null
      error_details TEXT, 
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );`
  _, err = d.Exec(createTableSQL)
  if err != nil {
    d.Close()
    return nil, fmt.Errorf("failed to create results table: %w", err)
  }

   // --- Schema Migration Example (Adding column idempotently) ---
   // Check if 'error_details' column exists
   rows, err := d.Query("PRAGMA table_info(results);")
   if err != nil {
       d.Close()
       return nil, fmt.Errorf("failed to query table info for results: %w", err)
   }
   defer rows.Close()

   columnExists := false
   for rows.Next() {
       var cid int
       var name string
       var type_ string
       var notnull int
       var dflt_value sql.NullString // Use sql.NullString for nullable default
       var pk int
       if err := rows.Scan(&cid, &name, &type_, &notnull, &dflt_value, &pk); err != nil {
           d.Close()
           return nil, fmt.Errorf("failed to scan table info row: %w", err)
       }
       if name == "error_details" {
           columnExists = true
           break
       }
   }
    if !columnExists {
        log.Println("Adding column 'error_details' to 'results' table.")
        _, err = d.Exec("ALTER TABLE results ADD COLUMN error_details TEXT;")
        if err != nil {
            d.Close()
            return nil, fmt.Errorf("failed to add error_details column: %w", err)
        }
    }
  // --- End Schema Migration Example ---


  log.Println("Database initialized successfully.")
  return d, nil
}
