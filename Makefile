APP?=mymatasan
APP_PATH=./apps/$(APP)
APP_CMD=./cmd/$(APP)
IMAGE=kopiv2:latest

.PHONY: help run test test-app test-mid test-bootstrap-mariadb docker-build up down logs

help:
	@echo "Available commands:"
	@echo "  make run APP=...  - Run selected app via root launcher"
	@echo "  make build APP=...- Build selected app binary only"
	@echo "  make test         - Run all tests"
	@echo "  make test-app     - Run selected app tests"
	@echo "  make test-mid     - Run middleware tests"
	@echo "  make test-bootstrap-mariadb - Run Docker-backed MariaDB bootstrap integration test"
	@echo "  make docker-build APP=... - Build docker image for selected app"
	@echo "  make up           - Start docker compose stack"
	@echo "  make down         - Stop docker compose stack"
	@echo "  make logs         - Tail docker compose logs"

run:
	go run . -app $(APP)

build:
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
