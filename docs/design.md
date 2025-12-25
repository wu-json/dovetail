# Dovetail Design

A Go service that exposes local services on a Tailnet using `tsnet`.

## Overview

Dovetail runs as a single container that joins your Tailnet and proxies incoming connections to backend services on the same Docker network. No public ports are exposed—all traffic flows through Tailscale's encrypted WireGuard tunnel.

## Architecture

```
                         ┌─────────────────────────────┐
                         │   Tailscale Coordination    │
                         │   (login.tailscale.com)     │
                         └─────────────┬───────────────┘
                                       │
                                       │ WireGuard tunnel
                                       │ (NAT traversal)
                                       │
┌──────────────────────────────────────┼────────────────────────────────────┐
│ Your Tailnet                         │                                    │
│                                      │                                    │
│  ┌─────────────┐  ┌─────────────┐    │                                    │
│  │ Laptop      │  │ Phone       │    │                                    │
│  │ 100.64.0.10 │  │ 100.64.0.11 │    │                                    │
│  └──────┬──────┘  └──────┬──────┘    │                                    │
│         │                │           │                                    │
│         │   Requests     │           │                                    │
│         │   over Tailnet │           │                                    │
│         ▼                ▼           ▼                                    │
│  ┌────────────────────────────────────────────────────────────────────┐   │
│  │ Docker Host                                                        │   │
│  │                                                                    │   │
│  │  ┌──────────────────────────────────────────────────────────────┐  │   │
│  │  │ Docker Network                                               │  │   │
│  │  │                                                              │  │   │
│  │  │  ┌─────────────────────────────────────────┐                 │  │   │
│  │  │  │ dovetail container       100.64.0.50    │                 │  │   │
│  │  │  │ ┌─────────────────────────────────────┐ │                 │  │   │
│  │  │  │ │ tsnet.Server                        │ │                 │  │   │
│  │  │  │ │  - Hostname: "services"             │ │                 │  │   │
│  │  │  │ │  - AuthKey: TS_AUTHKEY              │ │                 │  │   │
│  │  │  │ │  - Listen(:3000) ──────────────────────────► web:3000   │  │   │
│  │  │  │ │  - Listen(:8080) ──────────────────────────► api:8080   │  │   │
│  │  │  │ │  - Listen(:5432) ──────────────────────────► db:5432    │  │   │
│  │  │  │ └─────────────────────────────────────┘ │                 │  │   │
│  │  │  └─────────────────────────────────────────┘                 │  │   │
│  │  │                        │                                     │  │   │
│  │  │          ┌─────────────┼─────────────┐                       │  │   │
│  │  │          │             │             │                       │  │   │
│  │  │          ▼             ▼             ▼                       │  │   │
│  │  │   ┌──────────┐  ┌──────────┐  ┌──────────┐                   │  │   │
│  │  │   │   web    │  │   api    │  │    db    │                   │  │   │
│  │  │   │  :3000   │  │  :8080   │  │  :5432   │                   │  │   │
│  │  │   └──────────┘  └──────────┘  └──────────┘                   │  │   │
│  │  │                                                              │  │   │
│  │  └──────────────────────────────────────────────────────────────┘  │   │
│  └────────────────────────────────────────────────────────────────────┘   │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Request Flow

1. Client on Tailnet requests `services:3000` (or `100.64.0.50:3000`)
2. Tailscale routes through encrypted WireGuard tunnel
3. `tsnet.Server` in dovetail container receives request
4. Dovetail proxies to `web:3000` on Docker network
5. Response returns via same path

## Authentication

### Auth Keys (not OAuth)

Dovetail uses Tailscale **auth keys** for headless/Docker operation:

| | Auth Keys | OAuth Clients |
|---|---|---|
| **Purpose** | Register devices to Tailnet | Access Tailscale API |
| **Use case** | `tsnet.Server` joining network | Managing devices, ACLs, DNS |
| **What it does** | "Let this container be a node" | "Let this app call admin APIs" |

Generate auth key in: **Tailscale Admin → Settings → Keys → Generate auth key**

Recommended options:
- **Reusable** - same key works if container recreates
- **Ephemeral** (optional) - device auto-removes when offline
- **Tags** (optional) - e.g., `tag:docker` for ACL rules

### Sharing Auth Keys

A reusable auth key can register multiple containers, but each becomes a separate device:

```
Container A (TS_AUTHKEY=xxx) → Device "proxy-a" → 100.64.0.1
Container B (TS_AUTHKEY=xxx) → Device "proxy-b" → 100.64.0.2
```

For a single device exposing multiple services, run one dovetail instance with multiple listeners.

## State Management

### Ephemeral (stateless container)

```go
srv := &tsnet.Server{
    Hostname:  "services",
    AuthKey:   os.Getenv("TS_AUTHKEY"),
    Ephemeral: true,
}
```

- No volume needed
- Device disappears when container stops
- Use reusable + ephemeral auth key

### Persistent (survives restarts)

```go
srv := &tsnet.Server{
    Hostname: "services",
    AuthKey:  os.Getenv("TS_AUTHKEY"),
    Dir:      "/data/tsnet",
}
```

- Mount `/data/tsnet` as volume
- Device stays registered across restarts
- Auth key only needed on first run

## Tailscale Versioning

`tsnet` is part of the main Tailscale Go module. The version is baked into the binary at compile time:

```go
// go.mod
require tailscale.com v1.76.1
```

This includes:
- WireGuard implementation
- Tailscale coordination client
- DERP (relay) client
- MagicDNS resolver

Update with:
```bash
go get tailscale.com/tsnet@latest
go mod tidy
```

## Docker Deployment

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /dovetail .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /dovetail /dovetail
ENTRYPOINT ["/dovetail"]
```

### docker-compose.yml

```yaml
services:
  dovetail:
    build: .
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
      - SERVICES=web:3000,api:8080,db:5432
    volumes:
      - tsnet-state:/data/tsnet

  web:
    image: nginx

  api:
    image: my-api

  db:
    image: postgres

volumes:
  tsnet-state:
```

### Network Requirements

- Outbound internet access to Tailscale coordination servers
- No inbound ports required (Tailscale handles NAT traversal)
- `network_mode: bridge` (default) works fine

## Configuration

Environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `TS_AUTHKEY` | Yes (first run) | Tailscale auth key |
| `TS_HOSTNAME` | No | Device name on Tailnet (default: `dovetail`) |
| `SERVICES` | Yes | Comma-separated list of `backend:port` mappings |
| `TS_STATE_DIR` | No | State directory (default: `/data/tsnet`) |
| `TS_EPHEMERAL` | No | Set `true` for ephemeral mode |

## Security Considerations

- All traffic encrypted via WireGuard
- Only Tailnet members can reach exposed services
- Use Tailscale ACLs to restrict which devices can access which ports
- Backend services are not exposed to public internet
- Auth keys should be treated as secrets
