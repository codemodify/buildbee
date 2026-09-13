export GOTOOLCHAIN ?= local
ROOT := $(CURDIR)

.PHONY: web embed-web server build run test

web:
	cd web && npm ci && npm run build

# Copy Vite dist into the go:embed directory used by the Server binary.
embed-web: web
	rm -rf server/internal/webui/dist
	mkdir -p server/internal/webui/dist
	cp -a web/dist/. server/internal/webui/dist/
	touch server/internal/webui/dist/.gitkeep

server: embed-web
	mkdir -p bin
	cd server && go build -o $(ROOT)/bin/buildbee-server ./cmd/server

build: server

# Dev: Vite dist on disk, no embed required.
run: web
	cd server && BUILDBEE_WEB_DIR=$(ROOT)/web/dist go run ./cmd/server

test:
	./scripts/ci-local.sh
