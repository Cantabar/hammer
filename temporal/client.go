// temporal/client.go
package temporal

import (
  "log"

  "go.temporal.io/sdk/client"
)

// NewClient creates and returns a new Temporal client
func NewClient() (client.Client, error) {
  c, err := client.Dial(client.Options{
    HostPort: client.DefaultHostPort, // Assumes localhost:7233
    // Logger: Provide a custom logger if needed
  })
  if err != nil {
    log.Printf("Failed to create Temporal client: %v", err) // Log here instead of Fatalf
    return nil, err
  }
  log.Println("Temporal client connected successfully.")
  return c, nil
}
