# -----------------------------------------------------------------------------
# Stage 1: Build the Go application
# Use a Go base image for building
FROM golang:1.26.1 AS builder

# Set necessary environment variables for Go build
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

# Set the working directory inside the container
WORKDIR /app
RUN go env -w GOPROXY=https://goproxy.cn,direct
# Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the Go application
# We explicitly link with libX11.so.6 and libXcomposite.so.1 for Chrome,
# even though we use a different base for the final image.
# The final image will provide these.
RUN go build -o antinvo-go -ldflags "-s -w" .

# -----------------------------------------------------------------------------
# Stage 2: Create the final image
# Use a base image that includes Chrome (e.g., Browserless Chrome or a custom image)
# This example uses a common Browserless Chrome image.
# You might need to adjust this base image depending on your specific Chrome installation needs.
FROM selenium/standalone-chrome:nightly

# Set the working directory
WORKDIR /app

# Copy the built Go application from the builder stage
COPY --from=builder /app/antinvo-go .

# Expose the port your application listens on
EXPOSE 8080

# Command to run the application
ENTRYPOINT ["./antinvo-go"]