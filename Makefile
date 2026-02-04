.PHONY: build build-linux dev dev-restart dev-logs dev-down run test fmt vet lint clean

# Build for local macOS
build:
	go build -o total ./cmd/total

# Build for Linux (Docker containers)
build-linux:
	GOOS=linux GOARCH=arm64 go build -o total ./cmd/total

# Start dev environment (build + docker compose up)
dev: build-linux
	docker compose up -d
	@echo "Server running at http://localhost:8080"

# Restart dev after code changes (rebuild + restart containers)
dev-restart: build-linux
	docker compose restart server

# View dev logs
dev-logs:
	docker compose logs -f

# Stop dev environment
dev-down:
	docker compose down

# Run locally (loads .env via godotenv)
run: build
	./total

# Run tests
test:
	go test ./... -v

# Run short tests
test-short:
	go test ./... -short

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# Lint (format + vet)
lint: fmt vet

# Clean build artifacts
clean:
	rm -f total
	docker compose down -v
