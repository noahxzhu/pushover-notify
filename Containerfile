# Build Stage
FROM docker.io/library/golang:1.24-bookworm AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o pushover-notify ./cmd/server/main.go

# Run Stage
FROM docker.io/library/alpine:latest

WORKDIR /app

# Install timezone data and certificates
RUN apk --no-cache add tzdata ca-certificates

# Copy binary from builder
COPY --from=builder /app/pushover-notify .

# Copy config.yaml
COPY configs/config.yaml ./configs/config.yaml

# Create data directory
RUN mkdir -p data

# Expose port
EXPOSE 8089

# Define volume for data persistence
VOLUME ["/app/data"]

CMD ["./pushover-notify"]
