// Command eve-trader runs the EVE Trader web server.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
	"github.com/mgoodness/eve-trader/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := envOr("EVE_TRADER_ADDR", ":8080")
	dbPath := envOr("EVE_TRADER_DB_PATH", "eve-trader.db")

	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Bootstrap(context.Background()); err != nil {
		return err
	}

	// The real ESIClient (OAuth exchange/refresh and live ESI calls) lands
	// in #15; until then the fake keeps the HTTP -> ESIClient -> storage
	// path exercised end-to-end.
	srv := web.NewServer(esi.NewFakeClient(), store)

	log.Printf("listening on %s", addr)
	return http.ListenAndServe(addr, srv)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
