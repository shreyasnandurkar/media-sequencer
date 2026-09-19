// Command server is the media-sequencer backend.
//
// Startup order: config -> database -> migrations -> seed -> routes -> listen.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/api"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/config"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/events"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// A local .env is convenient on Windows; real env vars always take priority.
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("config loaded",
		"port", cfg.Port,
		"cycleMs", cfg.CycleMs,
		"syncLeadMs", cfg.SyncLeadMs,
		"allowedOrigins", cfg.AllowedOrigins,
		"seedOnStart", cfg.SeedOnStart)

	// signal.NotifyContext gives us a context that is cancelled on Ctrl+C or a
	// container stop; everything below hangs off it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	st, err := store.New(startupCtx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(startupCtx); err != nil {
		return err
	}
	if cfg.SeedOnStart {
		if err := st.Seed(startupCtx); err != nil {
			return err
		}
	}

	hub := events.NewHub(log)
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: api.NewServer(cfg, st, hub, log).Handler(),
		// No WriteTimeout: the SSE handler holds a response open indefinitely.
		// ReadHeaderTimeout still protects against slow-header attacks.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Run the listener on its own goroutine so main can wait for a signal.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Graceful shutdown: stop accepting new connections and let in-flight
	// requests finish. SSE streams are cancelled via their request contexts.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}
