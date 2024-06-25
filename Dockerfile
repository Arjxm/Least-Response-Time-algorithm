# Use the official Golang image
FROM golang:1.22.3

# Set the working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Run go mod tidy to ensure all dependencies are correctly recorded
RUN go mod tidy

# Build the application
RUN go build -o /load-balancer ./cmd/main.go

# Command to run the executable
ENTRYPOINT ["/load-balancer"]