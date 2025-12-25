# Dovetail Design Document

## Overview

Dovetail is a lightweight Go application that automatically exposes Docker containers to a Tailscale tailnet over HTTPS using Docker labels for configuration. Each labeled container gets its own service name on the tailnet (e.g., `https://myapp.me.ts.net`).

## Goals

- Minimal configuration: use Docker labels to declare intent
- Automatic discovery: watch for container start/stop events
- One service per container: each exposed container gets a unique tailnet hostname
- Simple deployment: runs as a Docker container

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Tailnet                              │
│   myapp1.me.ts.net    myapp2.me.ts.net    myapp3.me.ts.net │
└──────────┬───────────────────┬───────────────────┬──────────┘
           │                   │                   │
┌──────────┴───────────────────┴───────────────────┴──────────┐
│                         Dovetail                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │
│  │ TS Service  │  │ TS Service  │  │ TS Service  │          │
│  │   myapp1    │  │   myapp2    │  │   myapp3    │          │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘          │
│         │                │                │                 │
│  ┌──────┴────────────────┴────────────────┴──────┐          │
│  │              Reverse Proxy Layer              │          │
│  └──────┬────────────────┬────────────────┬──────┘          │
│         │                │                │                 │
│  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴──────┐          │
│  │ Container   │  │ Container   │  │ Container   │          │
│  │ Watcher     │  │ Watcher     │  │ Watcher     │          │
│  └─────────────┘  └─────────────┘  └─────────────┘          │
└─────────────────────────────────────────────────────────────┘
           │                   │                   │
┌──────────┴───────────────────┴───────────────────┴──────────┐
│                     Docker Daemon                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │
│  │ container1  │  │ container2  │  │ container3  │          │
│  │ :8080       │  │ :3000       │  │ :5432       │          │
│  └─────────────┘  └─────────────┘  └─────────────┘          │
└─────────────────────────────────────────────────────────────┘
```

## Docker Labels

Containers opt-in to exposure by setting a `dovetail.name` label. The presence of this label indicates the container should be exposed.

| Label | Required | Description | Example |
|-------|----------|-------------|---------|
| `dovetail.name` | Yes | Service name on the tailnet (presence enables exposure) | `myapp` |
| `dovetail.port` | Yes | Container port to expose | `8080` |

### Example Docker Compose

```yaml
version: "3.8"
services:
  webapp:
    image: nginx:latest
    labels:
      dovetail.name: "webapp"
      dovetail.port: "80"

  api:
    image: myapi:latest
    labels:
      dovetail.name: "api"
      dovetail.port: "3000"

  database:
    image: postgres:15
    # No dovetail.name label - not exposed to tailnet
```

### Example Docker Run

```bash
docker run -d \
  --label dovetail.name=myservice \
  --label dovetail.port=8080 \
  myimage:latest
```

## Components

### 1. Docker Watcher

Monitors Docker daemon for container events:

- **Start**: Check for `dovetail.name` label, register new Tailscale service if present
- **Stop/Die**: Tear down corresponding Tailscale service
- **Initial scan**: On startup, scan all running containers

```go
type DockerWatcher struct {
    client *docker.Client
    events chan ContainerEvent
}

type ContainerEvent struct {
    Type        EventType // Start, Stop
    ContainerID string
    Config      *ServiceConfig
}

type ServiceConfig struct {
    Name string // tailnet hostname
    Port int    // container port
    IP   string // container IP address
}
```

### 2. Tailscale Service Manager

Manages Tailscale services using `tsnet`:

```go
type ServiceManager struct {
    services map[string]*Service // keyed by container ID
    mu       sync.RWMutex
}

type Service struct {
    server    *tsnet.Server
    name      string
    targetURL string
    cancel    context.CancelFunc
}
```

Each service:
- Creates a `tsnet.Server` with the configured hostname
- Listens for incoming connections
- Proxies traffic to the container's internal IP and port

### 3. Reverse Proxy

HTTPS reverse proxy per service. Tailscale automatically provisions TLS certificates via MagicDNS:

```go
func (s *Service) startProxy(ctx context.Context) error {
    ln, err := s.server.ListenTLS("tcp", ":443")
    if err != nil {
        return err
    }

    proxy := &httputil.ReverseProxy{
        Director: func(req *http.Request) {
            req.URL.Scheme = "http"
            req.URL.Host = s.targetURL
        },
    }

    srv := &http.Server{Handler: proxy}
    go srv.Serve(ln)

    <-ctx.Done()
    return srv.Shutdown(context.Background())
}
```

TLS is terminated at the Tailscale service; traffic to containers uses HTTP over the internal Docker network.

## Configuration

Dovetail itself requires minimal configuration:

| Environment Variable | Required | Description |
|---------------------|----------|-------------|
| `TS_AUTHKEY` | Yes* | Tailscale auth key for new services |
| `TS_STATE_DIR` | No | Directory to persist Tailscale state (default: `/var/lib/dovetail`) |

*Can use `TS_AUTHKEY` or interactive auth on first run.

## Lifecycle

### Startup

1. Connect to Docker daemon
2. Scan running containers for `dovetail.name` label
3. For each labeled container, create Tailscale service
4. Start watching Docker events

### Container Start

1. Receive container start event
2. Inspect container for `dovetail.name` label
3. If present:
   - Extract name and port from labels
   - Get container IP from Docker network
   - Create new `tsnet.Server` with hostname
   - Start reverse proxy to container

### Container Stop

1. Receive container stop/die event
2. Look up service by container ID
3. If found:
   - Cancel service context
   - Close Tailscale server
   - Remove from service map

### Shutdown

1. Cancel all service contexts
2. Wait for graceful shutdown
3. Close Docker client

## State Management

Tailscale state is persisted per-service:

```
/var/lib/dovetail/
├── myapp1/
│   └── tailscaled.state
├── myapp2/
│   └── tailscaled.state
└── myapp3/
    └── tailscaled.state
```

This allows services to maintain their identity across restarts.

## Deployment

```yaml
version: "3.8"
services:
  dovetail:
    image: dovetail:latest
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - dovetail-state:/var/lib/dovetail
    restart: unless-stopped

volumes:
  dovetail-state:
```

## Error Handling

- **Docker connection lost**: Retry with exponential backoff
- **Container IP unavailable**: Skip container, log warning
- **Tailscale auth failure**: Log error, skip service creation
- **Port conflict**: Each service gets its own tsnet server, no conflicts

## Future Considerations

- TCP/UDP passthrough for non-HTTP services
- Health checks before exposing service
- Metrics endpoint for monitoring
- Support for multiple ports per container
