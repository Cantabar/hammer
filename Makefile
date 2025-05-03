# Makefile for the Hammer Project

# Use Go 1.11 modules
export GO111MODULE=on

# The .PHONY directive prevents make from confusing a target name with a file name
.PHONY: help up down logs run dev tidy check-docker

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

check-docker: ## Check if Docker daemon is running
	@if ! docker info > /dev/null 2>&1; then \
		echo "Error: Docker daemon is not running."; \
		exit 1; \
	fi

up: check-docker ## Start Docker containers if not already running/healthy
	# This command executes the check. If it fails (exit code 1), the OR || executes docker compose up -d.
	# If the check succeeds (exit code 0), the second command is skipped.
	docker compose up -d

down: check-docker ## Stop and remove Docker containers and volumes
	@echo "Stopping Docker containers..."
	docker compose down -v

logs: check-docker ## Tail logs from all Docker containers
	@echo "Tailing Docker logs... Press Ctrl+C to exit."
	docker compose logs -f

run: tidy ## Run the Go application directly (ensures dependencies are tidy first)
	@echo "Running Hammer Go application..."
	go run main.go

tidy: ## Tidy Go modules (run before building or running)
	@echo "Tidying Go modules..."
	go mod tidy

dev: tidy up ## Start Docker containers (if needed) and run mprocs (logs + Go app)
	@echo "Starting development environment with mprocs..."
	@echo "Press 'q' or Ctrl+C in mprocs window to quit."
	mprocs # Assumes mprocs.yaml or .mprocs.yaml exists
