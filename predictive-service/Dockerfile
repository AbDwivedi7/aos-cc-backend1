# predictive-service/Dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files first (for better Docker layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code
COPY . .

# Build the service (build ALL files in cmd/server/)
RUN CGO_ENABLED=0 GOOS=linux go build -o predictive-service ./cmd/server/

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/

COPY --from=builder /app/predictive-service .

EXPOSE 8080
CMD ["./predictive-service"]