// Command eve-trader runs the EVE Trader web server.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/mgoodness/eve-trader/internal/auth"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/notify"
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
	callbackURL := envOr("EVE_TRADER_CALLBACK_URL", "https://eve-trader.opsgoodness.net/auth/callback")
	dashboardURL := envOr("EVE_TRADER_DASHBOARD_URL", "https://eve-trader.opsgoodness.net/")

	// ESI Client ID/Secret and the Discord webhook URL are static secrets:
	// passed in as real environment variables via `docker run --env-file`
	// against a restricted-permission file on the host, never hardcoded or
	// committed here. The Discord webhook URL is optional -- leaving it
	// unset disables Discord Alert delivery (in-app Alerts still work).
	clientID := os.Getenv("EVE_TRADER_ESI_CLIENT_ID")
	clientSecret := os.Getenv("EVE_TRADER_ESI_CLIENT_SECRET")
	discordWebhookURL := os.Getenv("EVE_TRADER_DISCORD_WEBHOOK_URL")
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("EVE_TRADER_ESI_CLIENT_ID and EVE_TRADER_ESI_CLIENT_SECRET must be set")
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Bootstrap(context.Background()); err != nil {
		return err
	}

	esiClient := esi.NewRealClient(clientID, clientSecret)
	esiClient.Tokens = auth.NewTokenSource(esiClient, store)
	notifier := notify.New(discordWebhookURL, dashboardURL)
	srv := web.NewServer(esiClient, store, notifier, clientID, callbackURL)

	log.Printf("listening on %s", addr)
	return http.ListenAndServe(addr, srv)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
