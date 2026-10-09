package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goserver/internal/config"
	"goserver/internal/httpapi"
	"goserver/internal/logger"
	"goserver/internal/repository"
	"goserver/internal/service"
	"goserver/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server", slog.Any("err", err))
		os.Exit(1)
	}
}

// run returns rather than exits, so the deferred flushes still ship the error
// that stopped it.
func run() error {
	cfg := config.Parse()
	logger.Setup(cfg.LogFormat)
	if cfg.AdminToken != "" && len(cfg.AdminToken) < config.MinAdminTokenLen {
		return fmt.Errorf("ADMIN_TOKEN must be at least %d characters", config.MinAdminTokenLen)
	}

	tel, err := telemetry.Setup(context.Background())
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tel.Shutdown(ctx); err != nil {
			slog.Error("telemetry shutdown", slog.Any("err", err))
		}
	}()

	db, err := repository.OpenChecked(context.Background(), cfg.DatabaseURL, tel.BarmOptions()...)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(db, service.New(repository.NewCached(db)), cfg.AdminToken),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		// a fresh budget, not ctx: it is already done, that is why we are here
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx) //nolint:contextcheck
	}()

	slog.Info("listening", slog.String("addr", cfg.Addr))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
