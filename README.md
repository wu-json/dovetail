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

## Origin Story

I've recently fallen back into the [homelabbing](https://www.reddit.com/r/homelab/wiki/introduction/) rabbit hole and wanted to set up remote access to an [Immich](https://immich.app/) server for my photography work so I could travel and still look at stupid 4k cat photos on the go. In that search I discovered [tsdproxy](https://github.com/almeidapaulopt/tsdproxy), which worked but hasn't been updated in months, which made my anxiety-ridden brain melt a bit. Imagine my ass sitting sitting in a coffee shop in Asia losing access to my home server - how else am I supposed to generate images of cats doing the [海底捞 dance](https://www.reddit.com/r/TikTok/comments/1cnnikk/someone_please_explain_those_chinese_guys_that_do/)?

While searching for alternatives, I found [tsbridge](https://github.com/jtdowney/tsbridge) which honestly probably would have worked pretty well for my use-case, but since we have AI coding tools now I figured that making my own version with just the features relevant to me would be simple enough and a good learning opportunity for both myself and Anthropic.

I actually built and deployed this to my homelab in a few hours after a drunk Christmas dinner, and now I'm giving it to you... Merry Christmas.

### Cursed Thought
It's very empowering to generate custom software for yourself so easily now, but it also feels pretty weird right? Open-source serves as a critical foundation for training data for coding models, but as a result the threshold for justifying direct use of open-source projects has ballooned due to the significant decrease in cost to write and maintain. The litmus test of "can I write and maintain this" now reads positive more times than we are used to.

Ironically, there's something less personal about generating personalized software with AI coding tools. Running open-source is like running the author's heart on your machine. You're exposed to their personality, opinions, and decisions whether right or wrong, down to the very last bit. In a world where we skip the human and cherry pick the ideas and features we like, we detract ourselves from all of this, turning the art of sharing into a process of extraction.

It feels somewhat lonely.

That said my remote Immich access works now so maybe none of this matters.

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
