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

		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

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

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}
