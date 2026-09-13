// Command server starts the BuildBee Server (REST + WebSocket skeleton).
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/codemodify/buildbee/server/internal/httpapi"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("BUILDBEE_ADDR"); v != "" {
		addr = v
	}

	log.Printf("buildbee server listening on %s", addr)
	if err := http.ListenAndServe(addr, httpapi.NewMux()); err != nil {
		log.Fatal(err)
	}
}
