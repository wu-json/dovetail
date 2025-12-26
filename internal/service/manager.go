package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jasonwu/dovetail/internal/config"
	"github.com/jasonwu/dovetail/internal/docker"
)

// ServiceInterface abstracts Service operations for testing
type ServiceInterface interface {
	Start(ctx context.Context) error
	Stop() error
	UpdateTarget(ip string, port int) error
	Name() string
}

// ServiceFactory creates new services (for dependency injection in tests)
type ServiceFactory func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error)

// DefaultServiceFactory creates real Service instances
func DefaultServiceFactory(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
	return New(cfg, logger)
}

type Manager struct {
	config         *config.Config
	services       map[string]ServiceInterface // keyed by container ID
	names          map[string]string           // service name -> container ID (for duplicate detection)
	mu             sync.RWMutex
	logger         *slog.Logger
	serviceFactory ServiceFactory
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		config:         cfg,
		services:       make(map[string]ServiceInterface),
		names:          make(map[string]string),
		logger:         logger,
		serviceFactory: DefaultServiceFactory,
	}
}

// NewManagerWithFactory creates a Manager with a custom ServiceFactory (for testing)
func NewManagerWithFactory(cfg *config.Config, logger *slog.Logger, factory ServiceFactory) *Manager {
	return &Manager{
		config:         cfg,
		services:       make(map[string]ServiceInterface),
		names:          make(map[string]string),
		logger:         logger,
		serviceFactory: factory,
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
	svc, err := m.serviceFactory(&ServiceConfig{
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
	services := make([]ServiceInterface, 0, len(m.services))
	for _, svc := range m.services {
		services = append(services, svc)
	}
	m.services = make(map[string]ServiceInterface)
	m.names = make(map[string]string)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(s ServiceInterface) {
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
