// Command server starts the BuildBee Server (REST + Channel WebSocket).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/codemodify/buildbee/server/internal/httpapi"
	"github.com/codemodify/buildbee/server/internal/store"
	"github.com/codemodify/buildbee/server/internal/ws"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("BUILDBEE_ADDR"); v != "" {
		addr = v
	}

	ctx := context.Background()
	st, err := openStore(ctx)
	if err != nil {
		log.Fatal(err)
	}

	h := httpapi.NewServer(st, ws.NewHub()).Handler()
	log.Printf("buildbee server listening on %s", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatal(err)
	}
}

func openStore(ctx context.Context) (store.Store, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Print("DATABASE_URL unset; using in-memory store (not persisted)")
		return store.NewMemory(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	pool, err := store.OpenPostgres(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(ctx, pool); err != nil {
		return nil, err
	}
	log.Print("postgres connected; migrations applied")
	return store.NewPostgres(pool), nil
}
