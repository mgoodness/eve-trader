// Command eve-trader runs the single-user Rens station-trading
// opportunity finder described in docs/spec/v1.md.
package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgoodness/eve-trader/db"
	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/poller"
	"github.com/mgoodness/eve-trader/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("eve-trader exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := cmp.Or(os.Getenv("EVE_TRADER_DB_PATH"), "eve-trader.db")
	sqlDB, err := db.Open(dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// The gateway serves both public market data and authenticated ESI/SSO
	// calls through the same seam used by the server and background pollers.
	gateway := &esi.HTTPGateway{
		ClientID:    os.Getenv("EVE_TRADER_ESI_CLIENT_ID"),
		CallbackURL: cmp.Or(os.Getenv("EVE_TRADER_CALLBACK_URL"), "http://localhost:8080/auth/callback"),
	}
	orderPoller := poller.New(gateway, sqlDB, poller.DefaultInterval)
	go orderPoller.Run(ctx)
	historyPoller := poller.NewHistory(gateway, sqlDB)
	go historyPoller.Run(ctx)

	authConfig := server.AuthConfig{
		ClientID:     gateway.ClientID,
		CallbackURL:  gateway.CallbackURL,
		CookieSecret: os.Getenv("EVE_TRADER_COOKIE_SECRET"),
		TokenKey:     os.Getenv("EVE_TRADER_TOKEN_KEY"),
	}
	if authConfig.CookieSecret == "" || authConfig.TokenKey == "" {
		slog.Warn("EVE_TRADER_COOKIE_SECRET/EVE_TRADER_TOKEN_KEY not set; using ephemeral secrets for this process only -- in-flight logins and previously stored tokens won't survive a restart")
		if authConfig.CookieSecret == "" {
			secret, err := randomSecret()
			if err != nil {
				return err
			}
			authConfig.CookieSecret = secret
		}
		if authConfig.TokenKey == "" {
			secret, err := randomSecret()
			if err != nil {
				return err
			}
			authConfig.TokenKey = secret
		}
	}

	addr := cmp.Or(os.Getenv("EVE_TRADER_ADDR"), ":8080")
	serverHandler := server.New(gateway, sqlDB, authConfig)
	go serverHandler.NewSkillPoller(server.CharacterSkillsInterval).Run(ctx)
	srv := &http.Server{
		Addr:              addr,
		Handler:           serverHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("eve-trader listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// randomSecret returns a 32-byte cryptographically random value,
// hex-encoded, suitable as an ephemeral fallback for an AuthConfig
// secret when its environment variable isn't set.
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
