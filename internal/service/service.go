package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/jasonwu/dovetail/internal/proxy"
	"tailscale.com/tsnet"
)

type Service struct {
	name      string
	server    *tsnet.Server
	proxy     *proxy.Proxy
	targetURL *url.URL
	cancel    context.CancelFunc
	logger    *slog.Logger
	done      chan struct{}
}

type ServiceConfig struct {
	Name     string
	TargetIP string
	Port     int
	StateDir string
	AuthKey  string
}

func New(cfg *ServiceConfig, logger *slog.Logger) (*Service, error) {
	targetURL, err := url.Parse(fmt.Sprintf("http://%s:%d", cfg.TargetIP, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	server := &tsnet.Server{
		Hostname: cfg.Name,
		Dir:      filepath.Join(cfg.StateDir, cfg.Name),
		AuthKey:  cfg.AuthKey,
		Logf:     func(format string, args ...any) { logger.Debug(fmt.Sprintf(format, args...)) },
	}

	return &Service{
		name:      cfg.Name,
		server:    server,
		targetURL: targetURL,
		logger:    logger.With("service", cfg.Name),
		done:      make(chan struct{}),
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	// Start the tsnet server
	if err := s.server.Start(); err != nil {
		return fmt.Errorf("failed to start tsnet server: %w", err)
	}

	// Get local client for identity lookup
	lc, err := s.server.LocalClient()
	if err != nil {
		s.server.Close()
		return fmt.Errorf("failed to get local client: %w", err)
	}

	// Create proxy with identity injection
	s.proxy = proxy.New(s.targetURL, lc, s.logger)

	// Listen for HTTPS connections
	ln, err := s.server.ListenTLS("tcp", ":443")
	if err != nil {
		s.server.Close()
		return fmt.Errorf("failed to listen on TLS: %w", err)
	}

	httpServer := &http.Server{
		Handler:      s.proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start serving in background
	go func() {
		defer close(s.done)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("http server error", "error", err)
		}
	}()

	// Handle shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	s.logger.Info("service started", "hostname", s.name)
	return nil
}

func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for HTTP server to stop
	select {
	case <-s.done:
	case <-time.After(15 * time.Second):
		s.logger.Warn("timeout waiting for http server to stop")
	}

	if err := s.server.Close(); err != nil {
		return fmt.Errorf("failed to close tsnet server: %w", err)
	}

	s.logger.Info("service stopped")
	return nil
}

func (s *Service) UpdateTarget(ip string, port int) error {
	targetURL, err := url.Parse(fmt.Sprintf("http://%s:%d", ip, port))
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	s.targetURL = targetURL
	if s.proxy != nil {
		s.proxy.UpdateTarget(targetURL)
	}

	s.logger.Info("target updated", "ip", ip, "port", port)
	return nil
}

func (s *Service) Name() string {
	return s.name
}
