# Dovetail Design Document

## Overview

Dovetail is a lightweight Go application that automatically exposes Docker containers to a Tailscale tailnet over HTTPS using Docker labels for configuration. Each labeled container gets its own service name on the tailnet (e.g., `https://myapp.me.ts.net`).

## Goals

- Minimal configuration: use Docker labels to declare intent
- Automatic discovery: watch for container start/stop events
- One service per container: each exposed container gets a unique tailnet hostname
- Identity forwarding: inject Tailscale user info into requests for backend auth
- Simple deployment: runs as a Docker container

## Architecture

```
                            Tailnet
        ┌───────────────────────┬───────────────────────┐
        ▼                       ▼                       ▼
   myapp1.ts.net           myapp2.ts.net           myapp3.ts.net
        │                       │                       │
┌───────┴───────────────────────┴───────────────────────┴───────┐
│                           Dovetail                            │
│                                                               │
│  ┌─────────────┐       ┌────────────────────────────────────┐ │
│  │   Docker    │       │         Service Manager            │ │
│  │   Watcher   │──────▶│                                    │ │
│  │             │       │  ┌──────────┐ ┌──────────┐ ┌─────┐ │ │
│  │ (events)    │       │  │ Service  │ │ Service  │ │ ... │ │ │
│  └─────────────┘       │  │  myapp1  │ │  myapp2  │ │     │ │ │
│         │              │  ├──────────┤ ├──────────┤ ├─────┤ │ │
│         │              │  │  tsnet   │ │  tsnet   │ │     │ │ │
│         │              │  │  server  │ │  server  │ │     │ │ │
│         │              │  ├──────────┤ ├──────────┤ ├─────┤ │ │
│         │              │  │  reverse │ │  reverse │ │     │ │ │
│         │              │  │  proxy   │ │  proxy   │ │     │ │ │
│         │              │  └────┬─────┘ └────┬─────┘ └──┬──┘ │ │
│         │              └───────┼────────────┼─────────┼────┘ │
└─────────┼──────────────────────┼────────────┼─────────┼──────┘
          │                      │            │         │
          ▼                      ▼            ▼         ▼
┌──────────────────────────────────────────────────────────────┐
│                        Docker Daemon                         │
│                                                              │
│    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐     │
│    │ container1  │    │ container2  │    │ container3  │     │
│    │ :8080       │    │ :3000       │    │ :5432       │     │
│    └─────────────┘    └─────────────┘    └─────────────┘     │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

**Request flow:** Tailnet → tsnet server (TLS termination) → reverse proxy (adds identity headers) → container

**Event flow:** Docker daemon → Docker Watcher → Service Manager (create/update/destroy services)

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

            // Inject Tailscale identity headers
            whois, err := s.server.LocalClient().WhoIs(req.Context(), req.RemoteAddr)
            if err == nil && whois.UserProfile != nil {
                req.Header.Set("X-Tailscale-User", whois.UserProfile.LoginName)
                req.Header.Set("X-Tailscale-Name", whois.UserProfile.DisplayName)
                req.Header.Set("X-Tailscale-Login", whois.Node.ComputedName)
                req.Header.Set("X-Tailscale-Tailnet", whois.Node.Hostinfo.Hostname())
            }
        },
    }

    srv := &http.Server{Handler: proxy}
    go srv.Serve(ln)

    <-ctx.Done()
    return srv.Shutdown(context.Background())
}
```

TLS is terminated at the Tailscale service; traffic to containers uses HTTP over the internal Docker network.

### 4. Identity Headers

Dovetail injects Tailscale identity information into requests forwarded to backend services. This enables backends to authenticate users without implementing their own auth.

| Header | Description | Example |
|--------|-------------|---------|
| `X-Tailscale-User` | User's login email | `alice@example.com` |
| `X-Tailscale-Name` | User's display name | `Alice Smith` |
| `X-Tailscale-Login` | Node's computed name | `alice-macbook` |
| `X-Tailscale-Tailnet` | Tailnet identifier | `example.com` |

Backend services can trust these headers since traffic only arrives through the tailnet (already authenticated by Tailscale).

**Example usage in backend:**

```go
func handler(w http.ResponseWriter, r *http.Request) {
    user := r.Header.Get("X-Tailscale-User")
    if user == "" {
        http.Error(w, "unauthorized", 401)
        return
    }
    fmt.Fprintf(w, "Hello, %s!", user)
}
```

## Configuration

Dovetail itself requires minimal configuration:

| Environment Variable | Required | Description |
|---------------------|----------|-------------|
| `TS_AUTHKEY` | Yes* | Tailscale auth key for new services (must be reusable) |
| `TS_STATE_DIR` | No | Directory to persist Tailscale state (default: `/var/lib/dovetail`) |

*Can use `TS_AUTHKEY` or interactive auth on first run. The auth key must be [reusable](https://tailscale.com/kb/1085/auth-keys) since Dovetail creates a new Tailscale node for each exposed service.

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
- **Duplicate service name**: First container wins; subsequent containers with the same `dovetail.name` are skipped with an error log
- **Container IP change**: If a container's IP changes (e.g., after restart), the watcher detects this and updates the proxy target without recreating the Tailscale service

## Network Selection

When a container is connected to multiple Docker networks, Dovetail selects the target IP using the following priority:

1. Bridge network (if connected)
2. First available network (alphabetically)

## Future Considerations

- TCP/UDP passthrough for non-HTTP services
- Health checks before exposing service
- Metrics endpoint for monitoring
- Support for multiple ports per container
