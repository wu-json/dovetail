package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jasonwu/dovetail/internal/config"
	"github.com/jasonwu/dovetail/internal/docker"
)

// mockService implements ServiceInterface for testing
type mockService struct {
	name         string
	startCalled  bool
	stopCalled   bool
	startErr     error
	stopErr      error
	updateIP     string
	updatePort   int
	updateCalled bool
	updateErr    error
}

func (m *mockService) Start(ctx context.Context) error {
	m.startCalled = true
	return m.startErr
}

func (m *mockService) Stop() error {
	m.stopCalled = true
	return m.stopErr
}

func (m *mockService) UpdateTarget(ip string, port int) error {
	m.updateCalled = true
	m.updateIP = ip
	m.updatePort = port
	return m.updateErr
}

func (m *mockService) Name() string {
	return m.name
}

func TestNewManager(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	m := NewManager(cfg, logger)

	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.services == nil {
		t.Error("services map not initialized")
	}
	if m.names == nil {
		t.Error("names map not initialized")
	}
	if m.serviceFactory == nil {
		t.Error("serviceFactory not set")
	}
}

func TestNewManagerWithFactory(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	customFactory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return &mockService{name: cfg.Name}, nil
	}

	m := NewManagerWithFactory(cfg, logger, customFactory)

	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.serviceFactory == nil {
		t.Error("serviceFactory not set")
	}
}

func TestHandleEvent_Start(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	mock := &mockService{name: "testservice"}
	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		mock.name = cfg.Name
		return mock, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	event := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config: &docker.ServiceConfig{
			Name: "testservice",
			Port: 8080,
			IP:   "172.17.0.2",
		},
	}

	m.HandleEvent(context.Background(), event)

	if !mock.startCalled {
		t.Error("Start() was not called")
	}
	if m.ServiceCount() != 1 {
		t.Errorf("ServiceCount() = %d, want 1", m.ServiceCount())
	}
}

func TestHandleEvent_Start_NilConfig(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		t.Error("factory should not be called with nil config")
		return nil, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	event := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config:      nil,
	}

	m.HandleEvent(context.Background(), event)

	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0", m.ServiceCount())
	}
}

func TestHandleEvent_Start_DuplicateName(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	callCount := 0
	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		callCount++
		return &mockService{name: cfg.Name}, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	// First container
	event1 := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container111111111",
		Config: &docker.ServiceConfig{
			Name: "myservice",
			Port: 8080,
			IP:   "172.17.0.2",
		},
	}
	m.HandleEvent(context.Background(), event1)

	// Second container with same name
	event2 := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container222222222",
		Config: &docker.ServiceConfig{
			Name: "myservice",
			Port: 9090,
			IP:   "172.17.0.3",
		},
	}
	m.HandleEvent(context.Background(), event2)

	if callCount != 1 {
		t.Errorf("factory called %d times, want 1 (duplicate should be rejected)", callCount)
	}
	if m.ServiceCount() != 1 {
		t.Errorf("ServiceCount() = %d, want 1", m.ServiceCount())
	}
}

func TestHandleEvent_Start_ExistingContainer(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	mock := &mockService{name: "myservice"}
	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return mock, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	// Manually add a service to the manager to simulate an existing container
	m.mu.Lock()
	m.services["container123456789"] = mock
	// Note: We don't add to m.names to test the UpdateTarget path
	// (in real code, container IP changes might come via direct inspection)
	m.mu.Unlock()

	event := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config: &docker.ServiceConfig{
			Name: "newservice", // Different name to avoid duplicate check
			Port: 9090,
			IP:   "172.17.0.3",
		},
	}

	m.HandleEvent(context.Background(), event)

	if !mock.updateCalled {
		t.Error("UpdateTarget was not called for existing container")
	}
	if mock.updateIP != "172.17.0.3" {
		t.Errorf("updateIP = %q, want %q", mock.updateIP, "172.17.0.3")
	}
	if mock.updatePort != 9090 {
		t.Errorf("updatePort = %d, want %d", mock.updatePort, 9090)
	}
}

func TestHandleEvent_Start_FactoryError(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return nil, errors.New("factory error")
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	event := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config: &docker.ServiceConfig{
			Name: "myservice",
			Port: 8080,
			IP:   "172.17.0.2",
		},
	}

	m.HandleEvent(context.Background(), event)

	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0 (service creation failed)", m.ServiceCount())
	}
}

func TestHandleEvent_Start_StartError(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	mock := &mockService{name: "myservice", startErr: errors.New("start error")}
	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return mock, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	event := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config: &docker.ServiceConfig{
			Name: "myservice",
			Port: 8080,
			IP:   "172.17.0.2",
		},
	}

	m.HandleEvent(context.Background(), event)

	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0 (service start failed)", m.ServiceCount())
	}
}

func TestHandleEvent_Stop(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	mock := &mockService{name: "myservice"}
	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return mock, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	// First start a service
	startEvent := docker.ContainerEvent{
		Type:        docker.EventStart,
		ContainerID: "container123456789",
		Config: &docker.ServiceConfig{
			Name: "myservice",
			Port: 8080,
			IP:   "172.17.0.2",
		},
	}
	m.HandleEvent(context.Background(), startEvent)

	if m.ServiceCount() != 1 {
		t.Fatalf("ServiceCount() = %d, want 1 before stop", m.ServiceCount())
	}

	// Now stop it
	stopEvent := docker.ContainerEvent{
		Type:        docker.EventStop,
		ContainerID: "container123456789",
	}
	m.HandleEvent(context.Background(), stopEvent)

	if !mock.stopCalled {
		t.Error("Stop() was not called")
	}
	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0 after stop", m.ServiceCount())
	}
}

func TestHandleEvent_Stop_NonexistentContainer(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	m := NewManager(cfg, logger)

	// Stop a container that was never started
	stopEvent := docker.ContainerEvent{
		Type:        docker.EventStop,
		ContainerID: "container123456789",
	}

	// Should not panic
	m.HandleEvent(context.Background(), stopEvent)

	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0", m.ServiceCount())
	}
}

func TestShutdown(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return &mockService{
			name:    cfg.Name,
			stopErr: nil,
		}, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	// Start multiple services
	for i := 0; i < 3; i++ {
		event := docker.ContainerEvent{
			Type:        docker.EventStart,
			ContainerID: "container" + string(rune('A'+i)) + "123456789",
			Config: &docker.ServiceConfig{
				Name: "service" + string(rune('A'+i)),
				Port: 8080 + i,
				IP:   "172.17.0." + string(rune('2'+i)),
			},
		}
		m.HandleEvent(context.Background(), event)
	}

	if m.ServiceCount() != 3 {
		t.Fatalf("ServiceCount() = %d, want 3 before shutdown", m.ServiceCount())
	}

	// Override services with our tracked mocks
	m.mu.Lock()
	for id := range m.services {
		m.services[id] = &mockService{
			name: m.services[id].Name(),
			stopErr: nil,
		}
	}
	// Track stop calls
	for _, svc := range m.services {
		mock := svc.(*mockService)
		originalStop := mock.stopErr
		mock.stopErr = originalStop
	}
	m.mu.Unlock()

	m.Shutdown()

	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0 after shutdown", m.ServiceCount())
	}
}

func TestShutdown_ConcurrentStops(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	var stopCount atomic.Int32
	var mu sync.Mutex
	stoppedServices := make(map[string]bool)

	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return &mockService{name: cfg.Name}, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	// Start multiple services
	for i := 0; i < 5; i++ {
		event := docker.ContainerEvent{
			Type:        docker.EventStart,
			ContainerID: "container" + string(rune('A'+i)) + "123456789",
			Config: &docker.ServiceConfig{
				Name: "service" + string(rune('A'+i)),
				Port: 8080 + i,
				IP:   "172.17.0." + string(rune('2'+i)),
			},
		}
		m.HandleEvent(context.Background(), event)
	}

	// Replace with tracking mocks
	m.mu.Lock()
	for id, svc := range m.services {
		name := svc.Name()
		m.services[id] = &trackingMockService{
			name: name,
			onStop: func(n string) {
				stopCount.Add(1)
				mu.Lock()
				stoppedServices[n] = true
				mu.Unlock()
			},
		}
	}
	m.mu.Unlock()

	m.Shutdown()

	if got := stopCount.Load(); got != 5 {
		t.Errorf("stop called %d times, want 5", got)
	}
	if m.ServiceCount() != 0 {
		t.Errorf("ServiceCount() = %d, want 0 after shutdown", m.ServiceCount())
	}
}

// trackingMockService tracks stop calls for concurrent testing
type trackingMockService struct {
	name   string
	onStop func(name string)
}

func (t *trackingMockService) Start(ctx context.Context) error { return nil }
func (t *trackingMockService) Stop() error {
	if t.onStop != nil {
		t.onStop(t.name)
	}
	return nil
}
func (t *trackingMockService) UpdateTarget(ip string, port int) error { return nil }
func (t *trackingMockService) Name() string                           { return t.name }

func TestServiceCount(t *testing.T) {
	cfg := &config.Config{
		AuthKey:  "test-key",
		StateDir: "/tmp/test",
	}
	logger := slog.Default()

	factory := func(cfg *ServiceConfig, logger *slog.Logger) (ServiceInterface, error) {
		return &mockService{name: cfg.Name}, nil
	}

	m := NewManagerWithFactory(cfg, logger, factory)

	if m.ServiceCount() != 0 {
		t.Errorf("initial ServiceCount() = %d, want 0", m.ServiceCount())
	}

	// Add services
	for i := 0; i < 3; i++ {
		event := docker.ContainerEvent{
			Type:        docker.EventStart,
			ContainerID: "container" + string(rune('A'+i)) + "123456789",
			Config: &docker.ServiceConfig{
				Name: "service" + string(rune('A'+i)),
				Port: 8080 + i,
				IP:   "172.17.0.2",
			},
		}
		m.HandleEvent(context.Background(), event)

		expected := i + 1
		if m.ServiceCount() != expected {
			t.Errorf("ServiceCount() = %d, want %d", m.ServiceCount(), expected)
		}
	}
}
