# Dovetail 🕊️

Dovetail is a lightweight reverse proxy that automatically exposes Docker containers to your Tailscale tailnet over HTTPS. Simply add labels to your containers and they become accessible as secure endpoints on your private network.

## How It Works

```mermaid
flowchart TB
    subgraph tailnet["Your Tailnet"]
        user["You<br/><i>laptop.tailnet.ts.net</i>"]

        subgraph docker["Docker Host"]
            subgraph dovetail["Dovetail"]
                watcher["Docker Watcher"]
                manager["Service Manager"]
                watcher -->|"container events"| manager
            end

            subgraph services["Tailscale Proxies"]
                proxy1["webapp.tailnet.ts.net<br/><i>auto TLS</i>"]
                proxy2["api.tailnet.ts.net<br/><i>auto TLS</i>"]
            end

            subgraph containers["Docker Containers"]
                c1["webapp<br/><code>dovetail.port: 80</code>"]
                c2["api<br/><code>dovetail.port: 3000</code>"]
            end

            manager -->|"creates"| services
            watcher -.->|"discovers"| containers
        end
    end

    user -->|"https://webapp..."| proxy1
    user -->|"https://api..."| proxy2
    proxy1 -->|"+ X-Tailscale-User<br/>+ X-Tailscale-Name"| c1
    proxy2 -->|"+ X-Tailscale-User<br/>+ X-Tailscale-Name"| c2

    style tailnet fill:#f0f9ff,stroke:#0ea5e9
    style docker fill:#fefce8,stroke:#eab308
    style dovetail fill:#f0fdf4,stroke:#22c55e
    style services fill:#faf5ff,stroke:#a855f7
    style containers fill:#fff1f2,stroke:#f43f5e
```

**The Flow:**
1. Label your containers with `dovetail.name` and `dovetail.port`
2. Dovetail watches Docker and auto-discovers labeled containers
3. Each container gets its own Tailscale node with automatic TLS certificates
4. Requests are proxied with identity headers (`X-Tailscale-User`, `X-Tailscale-Name`, etc.)

## Key Features

- **Automatic Discovery**: Monitors Docker for container events and exposes labeled services
- **Zero Configuration TLS**: Tailscale handles certificate provisioning automatically
- **Identity Headers**: Injects Tailscale user info (`X-Tailscale-User`, `X-Tailscale-Name`, etc.) into proxied requests
- **Persistent Identity**: Services maintain their Tailscale identity across restarts

## Installation

```bash
docker pull ghcr.io/wu-json/dovetail:latest
```

## Usage

Add labels to containers you want to expose:

```yaml
services:
  webapp:
    image: nginx:latest
    labels:
      dovetail.name: "webapp"
      dovetail.port: "80"
```

Run dovetail alongside your containers:

```yaml
services:
  dovetail:
    image: ghcr.io/wu-json/dovetail:latest
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - dovetail-state:/var/lib/dovetail
    restart: unless-stopped
```

Your service will be available at `https://webapp.<tailnet-name>.ts.net`.

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TS_AUTHKEY` | Tailscale auth key (required, must be reusable) | - |
| `TS_STATE_DIR` | Directory for persisting Tailscale state | `/var/lib/dovetail` |

### Docker Labels

| Label | Required | Description |
|-------|----------|-------------|
| `dovetail.name` | Yes | Hostname for the service on your tailnet |
| `dovetail.port` | Yes | Container port to proxy |

