package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/api"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/config"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
)

var version = "0.1.0-dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("flowcanvasd stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadFromEnvironment()
	if err != nil {
		return err
	}
	if cfg.DatabasePath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o750); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	database, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	catalog := telemetry.NewMemoryCatalog(nil, telemetryDiscoveryNow())
	if cfg.DemoMode {
		catalog = telemetry.NewDemoCatalog(time.Now().UTC())
	}
	server := api.NewServer(database, catalog)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           server.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("flowcanvasd started", "version", version, "listen", cfg.ListenAddress, "demo", cfg.DemoMode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("flowcanvas API server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func telemetryDiscoveryNow() domain.DiscoveryStatus {
	now := time.Now().UTC()
	return domain.DiscoveryStatus{
		ConnectionsUpdatedAt: now,
		DevicesUpdatedAt:     now,
		TargetsUpdatedAt:     now,
	}
}
