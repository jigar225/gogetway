.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*##"; printf "\033[36m\033[0m"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps
deps: ## Install Go dependencies
	go mod download
	go mod tidy

.PHONY: deps-update
deps-update: ## Update Go dependencies
	go get -u ./...
	go mod tidy

.PHONY: build
build: ## Build the server binary
	go build -o bin/goget ./cmd/server/

.PHONY: build-worker
build-worker: ## Build the worker binary for background processing
	go build -o bin/goget-worker cmd/worker/main.go

.PHONY: build-all
build-all: build build-worker ## Build both server and worker binaries

.PHONY: run
run: ## Run the server locally
	go run cmd/server/main.go

.PHONY: run-worker
run-worker: ## Run the worker locally
	go run cmd/worker/main.go

.PHONY: dev
dev: ## Run the server locally
	go run cmd/server/main.go

.PHONY: dev-worker
dev-worker: ## Run the worker locally
	go run cmd/worker/main.go

##@ Docker

.PHONY: docker-up
docker-up: ## Start all dependencies with Docker Compose
	docker compose up -d --wait postgres redis
	@echo "Services are running:"
	@echo "  PostgreSQL: localhost:5432"
	@echo "  Redis: localhost:6380"

.PHONY: docker-down
docker-down: ## Stop all Docker services
	docker-compose down

.PHONY: docker-clean
docker-clean: ## Stop and remove all Docker containers and volumes
	docker-compose down -v

.PHONY: docker-logs
docker-logs: ## Show Docker logs
	docker-compose logs -f

.PHONY: docker-build
docker-build: ## Build Docker image for goget
	docker buildx build --platform linux/amd64 -t jigar225/goget:latest .

##@ Kubernetes & Helm
##
## Helm targets were removed as part of trimming (`deploy/` removed).

##@ Database

.PHONY: db-migrate
db-migrate: ## Run database migrations
	@echo "Migrations are auto-run on server start"

.PHONY: db-reset
db-reset: ## Reset database (drop and recreate)
	docker-compose exec postgres psql -U goget -c "DROP DATABASE IF EXISTS goget;"
	docker-compose exec postgres psql -U goget -c "CREATE DATABASE goget;"

.PHONY: db-shell
db-shell: ## Open PostgreSQL shell
	docker-compose exec postgres psql -U goget -d goget

.PHONY: redis-shell
redis-shell: ## Open Redis shell
	docker-compose exec redis redis-cli

##@ Testing

.PHONY: test
test: ## Run tests
	go test -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage
	go test -v -cover -coverprofile=coverage.txt ./...

.PHONY: test-integration
test-integration: ## Run integration tests
	go test -v -tags=integration ./...

.PHONY: test-failover
test-failover: ## Run failover and performance integration tests
	go test -v -timeout=60s ./internal/services/integration/ -run="Test"

.PHONY: test-performance
test-performance: ## Run performance benchmarks and validate banking requirements
	go test -v -timeout=60s ./internal/services/integration/ -run="TestPerformanceBenchmarks"

.PHONY: lint
lint: ## Run linter
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run

.PHONY: fmt
fmt: ## Format code
	go fmt ./...
	gofmt -s -w .

##@ Utilities

.PHONY: install-tools
install-tools: ## Install development tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/ tmp/ coverage.* *.out

.PHONY: env-setup
env-setup: ## Create .env file from example
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo ".env file created. Please update it with your API keys."; \
	else \
		echo ".env file already exists."; \
	fi

##@ Quick Start

.PHONY: setup
setup: env-setup deps install-tools docker-up ## Complete setup for development
	@echo "Setup complete! Now run 'make dev' to start the server"

.PHONY: start
start: docker-up run ## Start dependencies and run server

.PHONY: stop
stop: docker-down ## Stop everything

##@ API Testing

.PHONY: test-health
test-health: ## Test health endpoint
	curl -s http://localhost:8080/health | jq

.PHONY: test-register
test-register: ## Test user registration
	curl -X POST http://localhost:8080/v1/register \
		-H "Content-Type: application/json" \
		-d '{"email":"test@example.com","username":"testuser","password":"Test123!@#"}' | jq

.PHONY: test-login
test-login: ## Test user login
	curl -X POST http://localhost:8080/v1/login \
		-H "Content-Type: application/json" \
		-d '{"email":"test@example.com","password":"Test123!@#"}' | jq

.PHONY: test-chat
test-chat: ## Test chat completion (requires valid token)
	@echo "First login to get a token, then use it in the Authorization header"
	curl -X POST http://localhost:8080/v1/chat/completions \
		-H "Content-Type: application/json" \
		-H "Authorization: Bearer YOUR_TOKEN_HERE" \
		-d '{"model":"gpt-3.5-turbo","messages":[{"role":"user","content":"Hello!"}]}' | jq
