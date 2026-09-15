export GOTOOLCHAIN ?= local

BIN := bin

.PHONY: all web embed-web build run test vet clean

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

vet:
	go vet ./...

test: vet
	go test ./...
	cd web && npm ci && npm run build

clean:
	rm -rf $(BIN) web/dist
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
