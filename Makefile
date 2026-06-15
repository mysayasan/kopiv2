APP?=mymatasan
APP_PATH=./apps/$(APP)
APP_CMD=./cmd/$(APP)
WEB_DIR=$(APP_PATH)/views/react-webpack
IMAGE=kopiv2:latest

.PHONY: help web run run-go build build-go test test-app test-mid test-bootstrap-mariadb docker-build up down logs

help:
	@echo "Available commands:"
	@echo "  make run APP=...  - Build frontend bundle, then run app via root launcher"
	@echo "  make run-go APP=...- Run app WITHOUT rebuilding the frontend (Go-only changes)"
	@echo "  make web APP=...  - Rebuild the app's React bundle into apps/<app>/static"
	@echo "  make build APP=...- Build frontend bundle, then build app binary"
	@echo "  make test         - Run all tests"
	@echo "  make test-app     - Run selected app tests"
	@echo "  make test-mid     - Run middleware tests"
	@echo "  make test-bootstrap-mariadb - Run Docker-backed MariaDB bootstrap integration test"
	@echo "  make docker-build APP=... - Build docker image for selected app"
	@echo "  make up           - Start docker compose stack"
	@echo "  make down         - Stop docker compose stack"
	@echo "  make logs         - Tail docker compose logs"

# Rebuild the app's React bundle into apps/<app>/static. The server serves
# static assets from disk, so without this step a source change to the UI is
# never reflected in the running app — the stale-bundle trap. No-op for apps
# that have no frontend.
web:
	@if test -d "$(WEB_DIR)"; then \
		test -d "$(WEB_DIR)/node_modules" || (cd "$(WEB_DIR)" && npm install); \
		echo "Building frontend bundle for $(APP)..."; \
		(cd "$(WEB_DIR)" && npm run build); \
	else \
		echo "No frontend for $(APP); skipping web build"; \
	fi

run: web
	go run . -app $(APP)

# Run without rebuilding the frontend — use only when the UI bundle is current.
run-go:
	go run . -app $(APP)

build: web
	go build -trimpath -ldflags="-s -w" -o ./bin/$(APP)-server $(APP_CMD)

# Build the binary without rebuilding the frontend.
build-go:
	go build -trimpath -ldflags="-s -w" -o ./bin/$(APP)-server $(APP_CMD)

test:
	go test ./...

test-app:
	go test $(APP_PATH)

test-mid:
	go test ./domain/utils/middlewares

test-bootstrap-mariadb:
	RUN_MARIADB_IT=1 go test ./infra/db/bootstrap -run TestBootstrapEnsureMariaDBIntegration -v

docker-build:
	docker build --build-arg APP=$(APP) -t $(IMAGE)-$(APP) .

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f
