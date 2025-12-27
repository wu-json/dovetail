# Dovetail 🕊️

Dovetail is a lightweight reverse proxy that automatically exposes Docker containers to your Tailscale tailnet over HTTPS. Simply add labels to your containers and they become accessible as secure endpoints on your private network.

<table>
<tr>
<td width="300">
<img width="280" alt="スクリーンショット 2025-12-27 午前10 17 15" src="https://github.com/user-attachments/assets/e6c0a8a2-3bcf-478e-ae42-b1334ba2efc0" />
<br>
<sub>Art by <a href="https://www.instagram.com/p/DRRi4_OEsLo/">temo.scribbles</a> on Instagram</sub>
</td>
<td>

> *"The final destination for those who kept on living despite all of life's hardships shouldn't be localhost."*
>
> — Friebird the Slayer

</td>
</tr>
</table>

## Installation

```bash
docker pull wujson/dovetail:latest
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
    image: wujson/dovetail:latest
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./dovetail-state:/var/lib/dovetail
    restart: unless-stopped
```

Your service will be available at `https://webapp.<tailnet-name>.ts.net`.

## Docker Networking Requirements

**Important**: Dovetail must be able to reach your containers over the Docker network. This means they need to be on the same Docker network.

### Same docker-compose.yml (Automatic)

If Dovetail and your containers are defined in the same `docker-compose.yml` file, they automatically share a network. No additional configuration needed.

### Separate docker-compose.yml or `docker run` (Manual Setup)

If you're running Dovetail separately from your containers, you need to create a shared network:

**1. Create a shared network:**
```bash
docker network create dovetail-network
```

**2. Add Dovetail to the network:**
```yaml
services:
  dovetail:
    image: wujson/dovetail:latest
    environment:
      - TS_AUTHKEY=${TS_AUTHKEY}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./dovetail-state:/var/lib/dovetail
    networks:
      - dovetail-network
    restart: unless-stopped

networks:
  dovetail-network:
    external: true
```

**3. Add your containers to the same network:**
```yaml
services:
  myapp:
    image: nginx:latest
    labels:
      dovetail.name: "myapp"
      dovetail.port: "80"
    networks:
      - dovetail-network

networks:
  dovetail-network:
    external: true
```

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

## How It Works

```
                                  Tailnet
             ┌──────────────────────┬──────────────────────┐
             ▼                      ▼                      ▼
        myapp1.ts.net          myapp2.ts.net          myapp3.ts.net
             │                      │                      │
┌────────────┴──────────────────────┴──────────────────────┴────────────┐
│ Host                                                                  │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Dovetail                                                        │  │
│  │                                                                 │  │
│  │  ┌──────────────┐      ┌─────────────────────────────────────┐  │  │
│  │  │    Docker    │      │          Service Manager            │  │  │
│  │  │    Watcher   │─────▶│  ┌─────────┐ ┌─────────┐ ┌───────┐  │  │  │
│  │  └──────────────┘      │  │ myapp1  │ │ myapp2  │ │  ...  │  │  │  │
│  │         │              │  ├─────────┤ ├─────────┤ ├───────┤  │  │  │
│  │         │              │  │ tsnet   │ │ tsnet   │ │       │  │  │  │
│  │         │              │  ├─────────┤ ├─────────┤ ├───────┤  │  │  │
│  │         │              │  │ proxy   │ │ proxy   │ │       │  │  │  │
│  │         │              │  └────┬────┘ └────┬────┘ └───┬───┘  │  │  │
│  │         │              └───────┼───────────┼──────────┼──────┘  │  │
│  └─────────┼──────────────────────┼───────────┼──────────┼─────────┘  │
│            │                      │           │          │            │
│            ▼                      ▼           ▼          ▼            │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Docker                                                          │  │
│  │      ┌─────────────┐    ┌─────────────┐    ┌─────────────┐      │  │
│  │      │ container1  │    │ container2  │    │ container3  │      │  │
│  │      │    :8080    │    │    :3000    │    │    :5432    │      │  │
│  │      └─────────────┘    └─────────────┘    └─────────────┘      │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```
