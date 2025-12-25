package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jasonwu/dovetail/internal/config"
	"github.com/jasonwu/dovetail/internal/docker"
)

type Manager struct {
	config   *config.Config
	services map[string]*Service    // keyed by container ID
	names    map[string]string      // service name -> container ID (for duplicate detection)
	mu       sync.RWMutex
	logger   *slog.Logger
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		config:   cfg,
		services: make(map[string]*Service),
		names:    make(map[string]string),
		logger:   logger,
	}
}

func (m *Manager) HandleEvent(ctx context.Context, event docker.ContainerEvent) {
	switch event.Type {
	case docker.EventStart:
		m.handleStart(ctx, event)
	case docker.EventStop:
		m.handleStop(event)
	}
}

func (m *Manager) handleStart(ctx context.Context, event docker.ContainerEvent) {
	cfg := event.Config
	if cfg == nil {
		return
	}

	m.mu.Lock()

	// Check for duplicate service name
	if existingID, exists := m.names[cfg.Name]; exists {
		m.mu.Unlock()
		m.logger.Error("duplicate service name",
			"name", cfg.Name,
			"existing_container", existingID[:12],
			"new_container", event.ContainerID[:12],
		)
		return
	}

	// Check if we already have this container (e.g., from initial scan + event)
	if existing, exists := m.services[event.ContainerID]; exists {
		m.mu.Unlock()
		// Update target IP if it changed
		if err := existing.UpdateTarget(cfg.IP, cfg.Port); err != nil {
			m.logger.Error("failed to update service target", "error", err)
		}
		return
	}

	m.mu.Unlock()

	// Create and start new service
	svc, err := New(&ServiceConfig{
		Name:     cfg.Name,
		TargetIP: cfg.IP,
		Port:     cfg.Port,
		StateDir: m.config.StateDir,
		AuthKey:  m.config.AuthKey,
	}, m.logger)
	if err != nil {
		m.logger.Error("failed to create service",
			"name", cfg.Name,
			"container", event.ContainerID[:12],
			"error", err,
		)
		return
	}

	if err := svc.Start(ctx); err != nil {
		m.logger.Error("failed to start service",
			"name", cfg.Name,
			"container", event.ContainerID[:12],
			"error", err,
		)
		return
	}

	m.mu.Lock()
	m.services[event.ContainerID] = svc
	m.names[cfg.Name] = event.ContainerID
	m.mu.Unlock()

	m.logger.Info("service created",
		"name", cfg.Name,
		"container", event.ContainerID[:12],
		"target", fmt.Sprintf("%s:%d", cfg.IP, cfg.Port),
	)
}

func (m *Manager) handleStop(event docker.ContainerEvent) {
	m.mu.Lock()
	svc, exists := m.services[event.ContainerID]
	if !exists {
		m.mu.Unlock()
		return
	}

	delete(m.services, event.ContainerID)
	delete(m.names, svc.Name())
	m.mu.Unlock()

	if err := svc.Stop(); err != nil {
		m.logger.Error("failed to stop service",
			"name", svc.Name(),
			"container", event.ContainerID[:12],
			"error", err,
		)
	}

	m.logger.Info("service removed",
		"name", svc.Name(),
		"container", event.ContainerID[:12],
	)
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	services := make([]*Service, 0, len(m.services))
	for _, svc := range m.services {
		services = append(services, svc)
	}
	m.services = make(map[string]*Service)
	m.names = make(map[string]string)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			if err := s.Stop(); err != nil {
				m.logger.Error("failed to stop service during shutdown",
					"name", s.Name(),
					"error", err,
				)
			}
		}(svc)
	}
	wg.Wait()

	m.logger.Info("all services stopped")
}

func (m *Manager) ServiceCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.services)
}
