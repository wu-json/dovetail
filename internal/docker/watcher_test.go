package docker

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
)

// mockDockerClient implements DockerClient for testing
type mockDockerClient struct {
	containers      []types.Container
	containerJSON   types.ContainerJSON
	listErr         error
	inspectErr      error
	eventsChan      chan events.Message
	eventsErrChan   chan error
}

func (m *mockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.containers, nil
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.inspectErr != nil {
		return types.ContainerJSON{}, m.inspectErr
	}
	return m.containerJSON, nil
}

func (m *mockDockerClient) Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error) {
	return m.eventsChan, m.eventsErrChan
}

func (m *mockDockerClient) Close() error {
	return nil
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventStart, "start"},
		{EventStop, "stop"},
		{EventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.eventType.String(); got != tt.want {
				t.Errorf("EventType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetContainerIP(t *testing.T) {
	logger := slog.Default()
	w := NewWatcherWithClient(&mockDockerClient{}, logger)

	tests := []struct {
		name       string
		networks   map[string]*network.EndpointSettings
		wantIP     string
		wantNet    string
		wantErr    bool
	}{
		{
			name:     "no networks",
			networks: map[string]*network.EndpointSettings{},
			wantErr:  true,
		},
		{
			name: "bridge network takes priority",
			networks: map[string]*network.EndpointSettings{
				"custom": {IPAddress: "172.20.0.2"},
				"bridge": {IPAddress: "172.17.0.2"},
			},
			wantIP:  "172.17.0.2",
			wantNet: "bridge",
		},
		{
			name: "fallback to alphabetically first network",
			networks: map[string]*network.EndpointSettings{
				"zebra":  {IPAddress: "172.30.0.2"},
				"alpha":  {IPAddress: "172.20.0.2"},
			},
			wantIP:  "172.20.0.2",
			wantNet: "alpha",
		},
		{
			name: "skip networks without IP",
			networks: map[string]*network.EndpointSettings{
				"alpha": {IPAddress: ""},
				"beta":  {IPAddress: "172.20.0.2"},
			},
			wantIP:  "172.20.0.2",
			wantNet: "beta",
		},
		{
			name: "all networks without valid IP",
			networks: map[string]*network.EndpointSettings{
				"alpha": {IPAddress: ""},
				"beta":  {IPAddress: ""},
			},
			wantErr: true,
		},
		{
			name: "bridge network without IP falls back",
			networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: ""},
				"custom": {IPAddress: "172.20.0.2"},
			},
			wantIP:  "172.20.0.2",
			wantNet: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, netName, err := w.getContainerIP(tt.networks)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ip != tt.wantIP {
				t.Errorf("IP = %q, want %q", ip, tt.wantIP)
			}

			if netName != tt.wantNet {
				t.Errorf("network = %q, want %q", netName, tt.wantNet)
			}
		})
	}
}

func TestInspectContainer(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name          string
		containerJSON types.ContainerJSON
		inspectErr    error
		wantConfig    *ServiceConfig
		wantErr       bool
	}{
		{
			name:       "inspect error",
			inspectErr: errors.New("container not found"),
			wantErr:    true,
		},
		{
			name: "missing name label",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelPort: "8080",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty name label",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "",
						LabelPort: "8080",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing port label",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port value",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
						LabelPort: "not-a-number",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no network",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
						LabelPort: "8080",
					},
				},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			},
			wantErr: true,
		},
		{
			name: "valid container",
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
						LabelPort: "8080",
					},
				},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {IPAddress: "172.17.0.2"},
					},
				},
			},
			wantConfig: &ServiceConfig{
				Name:    "myservice",
				Port:    8080,
				IP:      "172.17.0.2",
				Network: "bridge",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDockerClient{
				containerJSON: tt.containerJSON,
				inspectErr:    tt.inspectErr,
			}
			w := NewWatcherWithClient(mock, logger)

			cfg, err := w.inspectContainer(context.Background(), "test-container-id")

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Name != tt.wantConfig.Name {
				t.Errorf("Name = %q, want %q", cfg.Name, tt.wantConfig.Name)
			}
			if cfg.Port != tt.wantConfig.Port {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.wantConfig.Port)
			}
			if cfg.IP != tt.wantConfig.IP {
				t.Errorf("IP = %q, want %q", cfg.IP, tt.wantConfig.IP)
			}
			if cfg.Network != tt.wantConfig.Network {
				t.Errorf("Network = %q, want %q", cfg.Network, tt.wantConfig.Network)
			}
		})
	}
}

func TestScanRunningContainers(t *testing.T) {
	logger := slog.Default()

	t.Run("list error logs and returns", func(t *testing.T) {
		mock := &mockDockerClient{
			listErr: errors.New("docker daemon not running"),
		}
		w := NewWatcherWithClient(mock, logger)
		events := make(chan ContainerEvent, 10)

		w.scanRunningContainers(context.Background(), events)

		select {
		case <-events:
			t.Error("expected no events on error")
		default:
			// expected - no events
		}
	})

	t.Run("emits start events for valid containers", func(t *testing.T) {
		mock := &mockDockerClient{
			containers: []types.Container{
				{ID: "container123456789"},
			},
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
						LabelPort: "8080",
					},
				},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {IPAddress: "172.17.0.2"},
					},
				},
			},
		}
		w := NewWatcherWithClient(mock, logger)
		events := make(chan ContainerEvent, 10)

		w.scanRunningContainers(context.Background(), events)

		select {
		case event := <-events:
			if event.Type != EventStart {
				t.Errorf("Type = %v, want %v", event.Type, EventStart)
			}
			if event.ContainerID != "container123456789" {
				t.Errorf("ContainerID = %q, want %q", event.ContainerID, "container123456789")
			}
			if event.Config == nil {
				t.Fatal("Config is nil")
			}
			if event.Config.Name != "myservice" {
				t.Errorf("Config.Name = %q, want %q", event.Config.Name, "myservice")
			}
		default:
			t.Error("expected event but got none")
		}
	})

	t.Run("skips containers that fail inspection", func(t *testing.T) {
		mock := &mockDockerClient{
			containers: []types.Container{
				{ID: "container123456789"},
			},
			inspectErr: errors.New("inspect failed"),
		}
		w := NewWatcherWithClient(mock, logger)
		events := make(chan ContainerEvent, 10)

		w.scanRunningContainers(context.Background(), events)

		select {
		case <-events:
			t.Error("expected no events when inspection fails")
		default:
			// expected - no events
		}
	})
}

func TestHandleEvent(t *testing.T) {
	logger := slog.Default()

	t.Run("start event with valid container", func(t *testing.T) {
		mock := &mockDockerClient{
			containerJSON: types.ContainerJSON{
				Config: &container.Config{
					Labels: map[string]string{
						LabelName: "myservice",
						LabelPort: "8080",
					},
				},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {IPAddress: "172.17.0.2"},
					},
				},
			},
		}
		w := NewWatcherWithClient(mock, logger)
		eventsChan := make(chan ContainerEvent, 10)

		msg := events.Message{
			Action: "start",
			Actor: events.Actor{
				ID: "container123",
			},
		}

		w.handleEvent(context.Background(), msg, eventsChan)

		select {
		case event := <-eventsChan:
			if event.Type != EventStart {
				t.Errorf("Type = %v, want %v", event.Type, EventStart)
			}
			if event.ContainerID != "container123" {
				t.Errorf("ContainerID = %q, want %q", event.ContainerID, "container123")
			}
		default:
			t.Error("expected event but got none")
		}
	})

	t.Run("start event with invalid container ignored", func(t *testing.T) {
		mock := &mockDockerClient{
			inspectErr: errors.New("no labels"),
		}
		w := NewWatcherWithClient(mock, logger)
		eventsChan := make(chan ContainerEvent, 10)

		msg := events.Message{
			Action: "start",
			Actor: events.Actor{
				ID: "container123",
			},
		}

		w.handleEvent(context.Background(), msg, eventsChan)

		select {
		case <-eventsChan:
			t.Error("expected no event for invalid container")
		default:
			// expected
		}
	})

	t.Run("stop event with dovetail label", func(t *testing.T) {
		mock := &mockDockerClient{}
		w := NewWatcherWithClient(mock, logger)
		eventsChan := make(chan ContainerEvent, 10)

		msg := events.Message{
			Action: "stop",
			Actor: events.Actor{
				ID: "container123",
				Attributes: map[string]string{
					LabelName: "myservice",
				},
			},
		}

		w.handleEvent(context.Background(), msg, eventsChan)

		select {
		case event := <-eventsChan:
			if event.Type != EventStop {
				t.Errorf("Type = %v, want %v", event.Type, EventStop)
			}
			if event.ContainerID != "container123" {
				t.Errorf("ContainerID = %q, want %q", event.ContainerID, "container123")
			}
		default:
			t.Error("expected event but got none")
		}
	})

	t.Run("die event with dovetail label", func(t *testing.T) {
		mock := &mockDockerClient{}
		w := NewWatcherWithClient(mock, logger)
		eventsChan := make(chan ContainerEvent, 10)

		msg := events.Message{
			Action: "die",
			Actor: events.Actor{
				ID: "container123",
				Attributes: map[string]string{
					LabelName: "myservice",
				},
			},
		}

		w.handleEvent(context.Background(), msg, eventsChan)

		select {
		case event := <-eventsChan:
			if event.Type != EventStop {
				t.Errorf("Type = %v, want %v", event.Type, EventStop)
			}
		default:
			t.Error("expected event but got none")
		}
	})

	t.Run("stop event without dovetail label ignored", func(t *testing.T) {
		mock := &mockDockerClient{}
		w := NewWatcherWithClient(mock, logger)
		eventsChan := make(chan ContainerEvent, 10)

		msg := events.Message{
			Action: "stop",
			Actor: events.Actor{
				ID:         "container123",
				Attributes: map[string]string{},
			},
		}

		w.handleEvent(context.Background(), msg, eventsChan)

		select {
		case <-eventsChan:
			t.Error("expected no event for container without dovetail label")
		default:
			// expected
		}
	})
}

func TestNewWatcherWithClient(t *testing.T) {
	logger := slog.Default()
	mock := &mockDockerClient{}

	w := NewWatcherWithClient(mock, logger)

	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
	if w.client != mock {
		t.Error("client not set correctly")
	}
	if w.logger != logger {
		t.Error("logger not set correctly")
	}
}
