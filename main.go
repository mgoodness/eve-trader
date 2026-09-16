// Command eve-trader runs the single-user Rens station-trading
// opportunity finder described in docs/spec/v1.md.
package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgoodness/eve-trader/db"
	"github.com/mgoodness/eve-trader/esi"
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

	// The real ESIGateway implementation (talking to live ESI/SSO) lands
	// in a later ticket; the fake keeps the skeleton runnable end to end
	// in the meantime.
	gateway := &esi.Fake{}

	addr := cmp.Or(os.Getenv("EVE_TRADER_ADDR"), ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(gateway, sqlDB),
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
