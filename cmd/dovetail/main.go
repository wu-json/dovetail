package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jasonwu/dovetail/internal/config"
	"github.com/jasonwu/dovetail/internal/docker"
	"github.com/jasonwu/dovetail/internal/service"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("starting dovetail", "version", version)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Create state directory if it doesn't exist
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		logger.Error("failed to create state directory", "path", cfg.StateDir, "error", err)
		os.Exit(1)
	}

	// Create Docker watcher
	watcher, err := docker.NewWatcher(logger)
	if err != nil {
		logger.Error("failed to create docker watcher", "error", err)
		os.Exit(1)
	}
	defer watcher.Close()

	// Create service manager
	manager := service.NewManager(cfg, logger)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Watch for container events
	events := watcher.Watch(ctx)

	logger.Info("watching for container events")

	// Process events
	for event := range events {
		logger.Debug("received event",
			"type", event.Type.String(),
			"container", event.ContainerID[:12],
		)
		manager.HandleEvent(ctx, event)
	}

	// Graceful shutdown
	logger.Info("shutting down services")
	manager.Shutdown()

	logger.Info("dovetail stopped")
}
