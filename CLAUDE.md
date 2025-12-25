# Dovetail

Dovetail automatically exposes Docker containers to your Tailscale network. It watches for containers with specific labels and creates dedicated Tailscale endpoints that proxy HTTPS traffic to them, injecting identity headers.

## Build & Test Commands

```bash
just build        # Build binary to dist/dovetail
just test         # Run all tests
just lint         # Run golangci-lint
just run          # Build and run
just clean        # Clean build artifacts
just docker-build # Build multi-arch Docker image
```

## Architecture

```
cmd/dovetail/main.go           # Entry point, signal handling, event loop
internal/config/config.go      # Environment-based configuration
internal/docker/watcher.go     # Docker event watching, container discovery
internal/service/service.go    # tsnet server + HTTP proxy per container
internal/service/manager.go    # Service lifecycle management
internal/proxy/proxy.go        # Reverse proxy with Tailscale identity injection
internal/version/version.go    # Version embedding
```

## Configuration

Environment variables:
- `TS_AUTHKEY` (required) - Tailscale auth key for registering nodes
- `TS_STATE_DIR` (optional) - State directory, default `/var/lib/dovetail`

Docker container labels:
- `dovetail.name` - Hostname for the Tailscale endpoint
- `dovetail.port` - Container port to proxy traffic to

## Key Behaviors

- Scans running containers on startup, then watches for start/stop events
- Each labeled container gets a dedicated tsnet server listening on :443 (TLS)
- Proxied requests include identity headers: `X-Tailscale-User`, `X-Tailscale-Name`, `X-Tailscale-Login`, `X-Tailscale-Tailnet`
- Duplicate service names are rejected (first container wins)
- Container IP changes trigger target updates without restarting the tsnet server
