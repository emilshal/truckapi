# Use the official Golang image as the base image
FROM golang:latest AS builder

# Move to working directory (/build)
WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod go.sum ./
RUN go mod tidy
RUN go mod download -x

# Copy the code into the container
COPY . .

# Set necessary environment variables needed for our image and build the API server
ENV CGO_ENABLED=1 GOOS=linux GOARCH=amd64
RUN go build -ldflags="-s -w" -o truckapi cmd/truckapi/main.go

# Debug step to list files in the builder stage
RUN ls -la /build

# Ensure the binary is executable
RUN chmod +x /build/truckapi

# Use Debian as the base image for the final stage
FROM debian:stable-slim

# Install necessary packages
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Set the working directory inside the container
WORKDIR /app

# Copy binary and config files from /build to root folder of the final image
COPY --from=builder /build/truckapi /app/truckapi
COPY --from=builder /build/.env /app/.env
COPY --from=builder /build/go.mod /app/go.mod

# Debug step to list files in the final stage
RUN ls -la /app

# Expose the necessary port
EXPOSE 8081

# Command to run when starting the container
ENTRYPOINT ["/app/truckapi"]
