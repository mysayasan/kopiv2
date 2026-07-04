APP?=mymatasan
APP_PATH=./apps/$(APP)
APP_CMD=./cmd/$(APP)
WEB_DIR=$(APP_PATH)/views/react-webpack
IMAGE=kopiv2:latest

# On Windows, GNU Make runs recipes under a stripped MSYS /bin/sh that carries
# almost none of the Windows env vars the Go toolchain relies on: no
# USERPROFILE/APPDATA (so Go can't locate GOPATH, its module cache, or its env
# file — "module cache not found: neither GOMODCACHE nor GOPATH is set") and no
# TMP/TEMP (so Go tries to create its build work dir under C:\Windows and dies
# with "Access is denied"). Rebuild the essential Windows vars from the real
# user profile — resolved via cygpath's CSIDL_PROFILE (id 40), which works
# regardless of the ambient env — so the toolchain behaves as in a normal
# terminal. No-op on Linux/macOS, where the shell already sets these.
ifneq (,$(findstring NT,$(shell uname -s)))
WINHOME := $(shell cygpath -m -F 40)
GOENV = USERPROFILE="$(WINHOME)" APPDATA="$(WINHOME)/AppData/Roaming" LOCALAPPDATA="$(WINHOME)/AppData/Local" TMP="$(WINHOME)/AppData/Local/Temp" TEMP="$(WINHOME)/AppData/Local/Temp"
# Windows executables need the .exe suffix. Go only auto-appends it when it
# derives the output name itself, not when we pass an explicit -o path.
EXT = .exe
else
GOENV =
EXT =
endif

.PHONY: help web run run-go build build-go stage test test-app test-mid test-bootstrap-mariadb docker-build up down logs

help:
	@echo "Available commands:"
	@echo "  make run APP=...  - Build frontend bundle, then run app via root launcher"
	@echo "  make run-go APP=...- Run app WITHOUT rebuilding the frontend (Go-only changes)"
	@echo "  make web APP=...  - Rebuild the app's React bundle into apps/<app>/static"
	@echo "  make build APP=...- Build frontend + binary, then stage config+static into bin/"
	@echo "  make stage APP=...- Copy config.json + static/ frontend into apps/<app>/bin/"
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
	$(GOENV) go run . -app $(APP)

# Run without rebuilding the frontend — use only when the UI bundle is current.
run-go:
	$(GOENV) go run . -app $(APP)

build: web
	$(GOENV) go build -trimpath -ldflags="-s -w" -o $(APP_PATH)/bin/$(APP)-server$(EXT) $(APP_CMD)
	@$(MAKE) --no-print-directory stage APP=$(APP)

# Build the binary without rebuilding the frontend.
build-go:
	$(GOENV) go build -trimpath -ldflags="-s -w" -o $(APP_PATH)/bin/$(APP)-server$(EXT) $(APP_CMD)
	@$(MAKE) --no-print-directory stage APP=$(APP)

# Copy runtime assets (config + built React frontend) next to the binary so
# $(APP_PATH)/bin is a self-contained, runnable bundle. Each is guarded so an
# app that lacks one just skips it. Portable cp/test — works on Windows/Linux/macOS.
stage:
	@test -f $(APP_PATH)/config.json && cp $(APP_PATH)/config.json $(APP_PATH)/bin/config.json && echo "Staged config.json" || echo "No $(APP_PATH)/config.json to stage"
	@test -d $(APP_PATH)/static && rm -rf $(APP_PATH)/bin/static && cp -r $(APP_PATH)/static $(APP_PATH)/bin/static && echo "Staged static/ frontend" || echo "No $(APP_PATH)/static to stage"
	@test -d $(APP_PATH)/ai && rm -rf $(APP_PATH)/bin/ai && cp -r $(APP_PATH)/ai $(APP_PATH)/bin/ai && rm -rf $(APP_PATH)/bin/ai/__pycache__ && echo "Staged ai/ scripts + model" || echo "No $(APP_PATH)/ai to stage"

test:
	$(GOENV) go test ./...

test-app:
	$(GOENV) go test $(APP_PATH)

test-mid:
	$(GOENV) go test ./domain/utils/middlewares

test-bootstrap-mariadb:
	$(GOENV) RUN_MARIADB_IT=1 go test ./infra/db/bootstrap -run TestBootstrapEnsureMariaDBIntegration -v

docker-build:
	docker build --build-arg APP=$(APP) -t $(IMAGE)-$(APP) .

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f
