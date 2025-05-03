# Makefile for the Hammer Project

# Use Go 1.11 modules
export GO111MODULE=on

# Define the expected running services based on docker-compose.yml
# Ensure these names match the service names in your docker-compose.yml
EXPECTED_SERVICES := postgres temporal temporal-ui

# Helper function (Make variable) to check if all expected services are running
# It compares the sorted list of running services with the sorted list of expected services.
# Returns shell exit code 0 if all are running, 1 otherwise.
define check_running_services
	@RUNNING_SERVICES=$$(docker compose ps --services --filter status=running | sort); \
	EXPECTED=$$(echo "$(EXPECTED_SERVICES)" | tr ' ' '\n' | sort); \
	if [ "$$RUNNING_SERVICES" = "$$EXPECTED" ]; then \
	  echo "All services ($(EXPECTED_SERVICES)) are already running."; \
	  exit 0; \
	else \
	  echo "One or more services ($(EXPECTED_SERVICES)) are not running. Executing 'docker compose up -d'..."; \
	  exit 1; \
	fi
endef

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
	@$(call check_running_services) || docker compose up -d

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
