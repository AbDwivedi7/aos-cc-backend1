# predictive-service/Makefile
.PHONY: build run test clean docker-build

# Variables
SERVICE_NAME := predictive-service
DOCKER_TAG := latest

# Build the service
build:
	go mod tidy
	go build -o bin/$(SERVICE_NAME) cmd/server/main.go cmd/server/handlers.go

# Run the service locally
run: build
	./bin/$(SERVICE_NAME)

# Run with live reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	air

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run integration tests
test-integration:
	go test -v -tags=integration ./tests/integration/...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Docker build
docker-build:
	docker build -t $(SERVICE_NAME):$(DOCKER_TAG) .

# Docker run (standalone)
docker-run: docker-build
	docker run -p 8080:8080 \
		-e REDIS_URL=host.docker.internal:6379 \
		-e NODE_API_URL=http://host.docker.internal:8081 \
		$(SERVICE_NAME):$(DOCKER_TAG)

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Generate mocks (requires mockgen)
mocks:
	go generate ./...

# Full system test with docker-compose
system-test:
	cd .. && docker-compose up --build -d
	sleep 10
	curl -f http://localhost:8080/health || (cd .. && docker-compose logs && exit 1)
	cd .. && docker-compose down
