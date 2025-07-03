# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy backend files
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# Build the application
RUN go build -o main .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose port
EXPOSE 8000

# Run the binary
CMD ["./main"]