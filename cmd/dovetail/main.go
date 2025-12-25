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

	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		logger.Error("failed to create state directory", "path", cfg.StateDir, "error", err)
		os.Exit(1)
	}

	watcher, err := docker.NewWatcher(logger)
	if err != nil {
		logger.Error("failed to create docker watcher", "error", err)
		os.Exit(1)
	}
	defer watcher.Close()

	manager := service.NewManager(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	events := watcher.Watch(ctx)

	logger.Info("watching for container events")

	for event := range events {
		logger.Debug("received event",
			"type", event.Type.String(),
			"container", event.ContainerID[:12],
		)
		manager.HandleEvent(ctx, event)
	}

	logger.Info("shutting down services")
	manager.Shutdown()

	logger.Info("dovetail stopped")
}
