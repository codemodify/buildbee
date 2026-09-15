export GOTOOLCHAIN ?= local

BIN := bin

.PHONY: all web embed-web build run test vet fmt-check smoke agents-image clean

all: build

web:
	cd web && npm ci && npm run build

# Copy the Vite build into the go:embed directory served by the Server.
embed-web: web
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -a web/dist/. internal/webui/dist/

build: embed-web
	mkdir -p $(BIN)
	go build -trimpath -o $(BIN)/buildbee-server ./cmd/buildbee-server
	go build -trimpath -o $(BIN)/buildbee-worker ./cmd/buildbee-worker
	go build -trimpath -o $(BIN)/buildbee ./cmd/buildbee

# Dev: serve the Vite build from disk instead of the embed.
run: web
	BUILDBEE_WEB_DIR=$(CURDIR)/web/dist go run ./cmd/buildbee-server

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run: gofmt -w ."; exit 1; }

vet:
	go vet ./...

# Go tests need Postgres: set BUILDBEE_TEST_DATABASE_URL, or have Docker
# available and a throwaway postgres container is started per package.
test: fmt-check vet
	go test ./...
	cd web && npm ci && npm run build

# Build both images and exercise the compose stack end to end.
smoke:
	./scripts/smoke-compose.sh

# Agents image for per-Run containers (BUILDBEE_WORKER_IMAGE).
agents-image:
	docker build -f Dockerfile.agents -t buildbee-agents .

clean:
	rm -rf $(BIN) web/dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
