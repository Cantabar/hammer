# Dockerfile
# Stage 1: Build the Go application
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
# Download dependencies
RUN go mod download && go mod verify

# Copy the source code
COPY . .

# Build the application statically
# CGO_ENABLED=0 is important for Alpine if using net package or anything potentially linking C libraries
# -ldflags="-s -w" strips debug info, reducing binary size
RUN CGO_ENABLED=0 go build -v -ldflags="-s -w" -o /app/server main.go

# Stage 2: Create the final lightweight image
FROM alpine:latest

# Install git and ca-certificates (git needed by go-git clone, ca-certs for HTTPS)
RUN apk update && apk add --no-cache git ca-certificates

WORKDIR /app

# Copy the static binary from the builder stage
COPY --from=builder /app/server /app/server
# Copy templates
COPY templates /app/templates
# Copy .env file (Alternatively, manage secrets via Docker secrets or env vars)
# COPY .env .env # Only if you want to bundle .env; usually set via compose

# Expose the application port
EXPOSE 3000

# Set the entrypoint command
# The application will read .env if present, or use system env vars
ENTRYPOINT ["/app/server"]
