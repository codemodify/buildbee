// Command server starts the BuildBee Server (REST + Channel WebSocket).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/routines"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/ws"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("BUILDBEE_ADDR"); v != "" {
		addr = v
	} else if p := os.Getenv("PORT"); p != "" {
		if !strings.HasPrefix(p, ":") {
			p = ":" + p
		}
		addr = p
	}

	ctx := context.Background()
	st, err := openStore(ctx)
	if err != nil {
		log.Fatal(err)
	}

	go routines.StartWorker(ctx, st, 15*time.Second)
	h := httpapi.NewServer(st, ws.NewHub()).Handler()
	log.Printf("buildbee server listening on %s", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatal(err)
	}
}

func openStore(ctx context.Context) (store.Store, error) {
	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if url == "" {
		return nil, errors.New("DATABASE_URL is required (Postgres is the only store)")
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
