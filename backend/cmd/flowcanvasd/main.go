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
	"sync"
	"syscall"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/api"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/config"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/topology"
)

var version = "0.2.0-dev"

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

	var catalog telemetry.Catalog
	var liveCatalog *telemetry.LiveCatalog
	if cfg.DemoMode {
		catalog = telemetry.NewDemoCatalog(time.Now().UTC())
	} else {
		if closed, err := database.MarkOpenConnectionsInactive(ctx, time.Now().UTC()); err != nil {
			return fmt.Errorf("recover stale Mihomo connections: %w", err)
		} else if closed > 0 {
			logger.Warn("marked stale connection samples inactive after restart", "connections", closed)
		}
		liveCatalog = telemetry.NewLiveCatalog(database)
		catalog = liveCatalog
	}

	server := api.NewServer(database, catalog)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           server.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	var workers sync.WaitGroup
	if !cfg.DemoMode {
		if err := startLiveWorkers(ctx, &workers, logger, cfg, database, server, liveCatalog, stop); err != nil {
			return err
		}
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
	httpErr := httpServer.Shutdown(shutdownCtx)
	workers.Wait()
	return httpErr
}

func startLiveWorkers(
	ctx context.Context,
	workers *sync.WaitGroup,
	logger *slog.Logger,
	cfg config.Config,
	database *store.Store,
	server *api.Server,
	liveCatalog *telemetry.LiveCatalog,
	stop context.CancelFunc,
) error {
	writer, err := telemetry.NewFeatureWriter(database, telemetry.FeatureWriterConfig{
		QueueCapacity: cfg.FeatureQueueCapacity,
		BatchSize:     cfg.FeatureBatchSize,
		FlushInterval: cfg.FeatureFlushInterval,
	}, func(result store.FeatureApplyResult) {
		server.NotifyDiscoveryChanged("connections")
		logger.Debug("persisted Mihomo feature batch", "observed", result.Observed, "closed", result.Closed)
	})
	if err != nil {
		return fmt.Errorf("create feature event writer: %w", err)
	}
	client, err := mihomo.NewClient(cfg.MihomoController, cfg.MihomoSecret, nil)
	if err != nil {
		return fmt.Errorf("create Mihomo controller client: %w", err)
	}
	watcher := mihomo.NewWatcher(client, writer, cfg.ConnectionInterval)
	topologyRefresher, err := topology.NewRefresher(
		database,
		cfg.ARPPath,
		cfg.DHCPLeasePath,
		func(changed bool, observedAt time.Time) {
			liveCatalog.MarkDevicesRefreshed(observedAt)
			if changed {
				server.NotifyDiscoveryChanged("topology")
			}
		},
		func(err error) { logger.Warn("topology refresh failed", "error", err) },
	)
	if err != nil {
		return fmt.Errorf("create topology refresher: %w", err)
	}
	proxyRefresher, err := telemetry.NewProxyRefresher(
		client,
		liveCatalog,
		func(_ []domain.Target, _ time.Time) { server.NotifyDiscoveryChanged("targets") },
		func(err error) { logger.Warn("Mihomo proxy catalog refresh failed", "error", err) },
	)
	if err != nil {
		return fmt.Errorf("create Mihomo proxy refresher: %w", err)
	}
	server.SetDiscoveryRefreshHandler(func(refreshCtx context.Context) error {
		if _, err := topologyRefresher.Refresh(refreshCtx); err != nil {
			return fmt.Errorf("refresh topology: %w", err)
		}
		if _, err := proxyRefresher.Refresh(refreshCtx); err != nil {
			return fmt.Errorf("refresh Mihomo proxy catalog: %w", err)
		}
		return nil
	})

	workers.Add(4)
	go func() {
		defer workers.Done()
		if err := writer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("feature writer stopped", "error", err)
			stop()
		}
	}()
	go func() {
		defer workers.Done()
		if err := watcher.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("Mihomo connection watcher stopped", "error", err)
			stop()
		}
	}()
	go func() {
		defer workers.Done()
		topologyRefresher.Run(ctx, cfg.TopologyInterval)
	}()
	go func() {
		defer workers.Done()
		proxyRefresher.Run(ctx, cfg.ProxyRefreshInterval)
	}()
	return nil
}

func telemetryDiscoveryNow() domain.DiscoveryStatus {
	now := time.Now().UTC()
	return domain.DiscoveryStatus{
		ConnectionsUpdatedAt: now,
		DevicesUpdatedAt:     now,
		TargetsUpdatedAt:     now,
	}
}
