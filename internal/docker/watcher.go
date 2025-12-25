package docker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	LabelName    = "dovetail.name"
	LabelPort    = "dovetail.port"
	LabelNetwork = "dovetail.network"
)

type EventType int

const (
	EventStart EventType = iota
	EventStop
)

func (e EventType) String() string {
	switch e {
	case EventStart:
		return "start"
	case EventStop:
		return "stop"
	default:
		return "unknown"
	}
}

type ServiceConfig struct {
	Name    string
	Port    int
	IP      string
	Network string
}

type ContainerEvent struct {
	Type        EventType
	ContainerID string
	Config      *ServiceConfig
}

type Watcher struct {
	client *client.Client
	logger *slog.Logger
}

func NewWatcher(logger *slog.Logger) (*Watcher, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Watcher{
		client: cli,
		logger: logger,
	}, nil
}

func (w *Watcher) Close() error {
	return w.client.Close()
}

func (w *Watcher) Watch(ctx context.Context) <-chan ContainerEvent {
	events := make(chan ContainerEvent)

	go func() {
		defer close(events)

		// First, scan running containers
		w.scanRunningContainers(ctx, events)

		// Then watch for new events
		w.watchEvents(ctx, events)
	}()

	return events
}

func (w *Watcher) scanRunningContainers(ctx context.Context, events chan<- ContainerEvent) {
	containers, err := w.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelName),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		w.logger.Error("failed to list containers", "error", err)
		return
	}

	for _, c := range containers {
		cfg, err := w.inspectContainer(ctx, c.ID)
		if err != nil {
			w.logger.Warn("failed to inspect container", "id", c.ID[:12], "error", err)
			continue
		}

		events <- ContainerEvent{
			Type:        EventStart,
			ContainerID: c.ID,
			Config:      cfg,
		}
	}
}

func (w *Watcher) watchEvents(ctx context.Context, eventsChan chan<- ContainerEvent) {
	filterArgs := filters.NewArgs(
		filters.Arg("type", "container"),
		filters.Arg("event", "start"),
		filters.Arg("event", "stop"),
		filters.Arg("event", "die"),
	)

	msgChan, errChan := w.client.Events(ctx, events.ListOptions{Filters: filterArgs})

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errChan:
			if err != nil && ctx.Err() == nil {
				w.logger.Error("docker events error", "error", err)
			}
			return
		case msg := <-msgChan:
			w.handleEvent(ctx, msg, eventsChan)
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, msg events.Message, eventsChan chan<- ContainerEvent) {
	switch msg.Action {
	case "start":
		cfg, err := w.inspectContainer(ctx, msg.Actor.ID)
		if err != nil {
			// Container might not have dovetail labels, which is fine
			return
		}
		eventsChan <- ContainerEvent{
			Type:        EventStart,
			ContainerID: msg.Actor.ID,
			Config:      cfg,
		}

	case "stop", "die":
		// For stop/die events, we don't need the full config
		// Just check if it had our label (from the event attributes)
		if _, ok := msg.Actor.Attributes[LabelName]; ok {
			eventsChan <- ContainerEvent{
				Type:        EventStop,
				ContainerID: msg.Actor.ID,
			}
		}
	}
}

func (w *Watcher) inspectContainer(ctx context.Context, id string) (*ServiceConfig, error) {
	info, err := w.client.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	// Check for required labels
	name, ok := info.Config.Labels[LabelName]
	if !ok || name == "" {
		return nil, fmt.Errorf("container missing %s label", LabelName)
	}

	portStr, ok := info.Config.Labels[LabelPort]
	if !ok || portStr == "" {
		return nil, fmt.Errorf("container missing %s label", LabelPort)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port value %q: %w", portStr, err)
	}

	// Get preferred network from label
	preferredNetwork := info.Config.Labels[LabelNetwork]

	// Get container IP
	ip, network, err := w.getContainerIP(info.NetworkSettings.Networks, preferredNetwork)
	if err != nil {
		return nil, err
	}

	w.logger.Info("discovered container",
		"id", id[:12],
		"name", name,
		"port", port,
		"ip", ip,
		"network", network,
	)

	return &ServiceConfig{
		Name:    name,
		Port:    port,
		IP:      ip,
		Network: network,
	}, nil
}

func (w *Watcher) getContainerIP(networks map[string]*network.EndpointSettings, preferred string) (string, string, error) {
	if len(networks) == 0 {
		return "", "", fmt.Errorf("container has no networks")
	}

	// Priority 1: Use preferred network if specified and available
	if preferred != "" {
		if net, ok := networks[preferred]; ok && net.IPAddress != "" {
			return net.IPAddress, preferred, nil
		}
		w.logger.Warn("preferred network not found or has no IP", "network", preferred)
	}

	// Priority 2: Use bridge network
	if net, ok := networks["bridge"]; ok && net.IPAddress != "" {
		return net.IPAddress, "bridge", nil
	}

	// Priority 3: First available network (alphabetically for consistency)
	var names []string
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if ip := networks[name].IPAddress; ip != "" {
			return ip, name, nil
		}
	}

	return "", "", fmt.Errorf("no network with valid IP found")
}
