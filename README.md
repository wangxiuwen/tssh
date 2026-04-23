# tssh (Terminal SSH for Aliyun)

`tssh` is an open-source, **zero external dependency**, single-binary CLI tool for managing Alibaba Cloud (Aliyun) ECS instances. No SSH keys, no bastion hosts, no `ali-instance-cli` — everything runs natively via Cloud Assistant WebSocket APIs.

When managing a fleet of hundreds of servers without public IP addresses, the traditional workflow of hunting down internal IPs, proxying through Bastion hosts (Jump Servers), and distributing SSH keys becomes incredibly cumbersome and inefficient. `tssh` eliminates all of this overhead. By natively leveraging Aliyun's Cloud Assistant and Cloud Shell WebSocket APIs, it establishes secure tunnels directly to your instances using only your Aliyun API credentials.

### 🤖 AI Agent Friendly

`tssh` is purpose-built for programmatic consumption by AI agents and automation pipelines:
- **JSON output** (`-j`/`--json`) on all commands for structured parsing
- **stdin pipe support** — pipe multi-line scripts directly: `echo 'script' | tssh exec name -`
- **Script file execution** — `tssh exec -s deploy.sh name`
- **Exit code passthrough** — remote exit codes propagate to `$?`
- **Quiet mode** (`-q`) — suppress all decoration, output only results
- **High-performance batch** — parallel execution with rate limiting and automatic throttle retry

[中文文档 (Chinese)](README_zh.md)

## Features

- **Interactive Terminal:** Full WebSocket-based pseudo-terminal via Cloud Assistant.
- **Interactive Search:** FZF-like real-time fuzzy search by instance Name, ID, or IP.
- **Index-based Login:** Fast-connect via numerical index (`tssh 5`).
- **Remote Execution:** Single or batch execution with concurrent workers and structured output.
- **Connectivity Test:** Quick ping via Cloud Assistant (`tssh ping`).
- **Health Inspection:** Deep server health check with anomaly detection — CPU, memory, disk, JVM, OOM, TIME_WAIT (`tssh health`).
- **Instance Details:** Show full instance specs, CPU/Memory/OS/VPC/Security Groups (`tssh info`).
- **Self-Diagnostics:** Check credentials, API connectivity, cache status (`tssh doctor`).
- **Self-Update:** Automatic update from GitHub Releases (`tssh update`).
- **Remote Log Tailing:** Follow remote logs in real-time (`tssh tail`).
- **Periodic Monitoring:** Watch command output with auto-refresh (`tssh watch`).
- **Live Dashboard:** Real-time instance monitoring panel (`tssh top`).
- **Multi-Instance Diff:** Compare command output across machines with color diff (`tssh diff`).
- **Instance Lifecycle:** Stop, start, reboot instances with status polling (`tssh stop/start/reboot`).
- **Persistent Tunnels:** Manage long-running port forwarding tunnels (`tssh tunnel start/list/stop`).
- **Web Management UI:** Embedded dark-themed web dashboard with token auth (`tssh web --token <tok>`).
- **Webhook Notifications:** DingTalk, Feishu, Slack, and generic webhook support.
- **Port Forwarding Sugar:** Shorthand syntax — `tssh -L 3306 host` equals `-L 3306:localhost:3306`.
- **Resumable Transfers:** Large file transfer with rsync `--partial` for resume on interruption (`tssh cp --resume`).
- **Multi-Account Profiles:** Manage multiple Aliyun accounts via `~/.tssh/config.json` (`--profile`).
- **SSH-Compatible Flags:** Accepts standard SSH flags (`-l`, `-p`, `-i`, `-o`, etc.) for drop-in compatibility.
- **JSON Output:** Machine-readable output for all commands (`-j`/`--json`).
- **Stdin / Script Input:** Pipe scripts via stdin or execute from file (`-s`).
- **Batch Execution:** Concurrent execution with keyword match (`-g`), tag filter (`--tag`), progress tracking (`--progress`).
- **Exit Code Passthrough:** Remote command exit codes propagate to the local process.
- **File Transfer:** Send files via Cloud Assistant API (`tssh cp`), with SCP fallback for large files (>32KB).
- **Port Forwarding:** Local port tunnels with remote host relay support (`tssh -L 8080:remote:80 <name>`).
- **Dev "VPN" suite:** Access internal services from localhost without a bastion SSH key — `tssh fwd` (one port, zero config), `tssh run` (multi-port + env injection, Spring-friendly), `tssh socks` (SOCKS5 proxy), `tssh shell` (subshell with preset `ALL_PROXY`/`JAVA_TOOL_OPTIONS`), `tssh vpn` (L3 TUN transparent proxy for Kafka/MQ/gRPC).
- **rsync Support:** Native rsync via tunnel (`trsync`).
- **API Rate Limiting:** Built-in rate limiter and automatic retry on API throttling.
- **ARMS Monitoring:** One-click alert inspection, Grafana dashboard access, Prometheus queries, and distributed trace lookup by TraceID / custom tag (e.g. `globalId`) via `tssh arms`.
- **Shell Completion:** Bash and Zsh completion support (`tssh completion`).
- **Execution History:** Track past commands (`tssh history`).
- **SSH Config Generation:** Generate `~/.ssh/config` entries (`tssh ssh-config`).
- **Auto Cache Refresh:** Stale cache (>24h) refreshes silently in background.

## Prerequisites

Aliyun RAM AccessKeys. Credentials are searched in this order:
1. Environment variables (`ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_REGION_ID`)
2. tssh multi-account config (`~/.tssh/config.json`)
3. Aliyun CLI config (`~/.aliyun/config.json`)

## Installation

### Option 1 — full toolkit

Download pre-built binaries from [GitHub Releases](https://github.com/wangxiuwen/tssh/releases), or compile from source:

```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
make build
sudo cp tssh /usr/local/bin/
```

### Option 2 — slim binaries by capability

tssh ships as a family of small single-responsibility binaries built from the
same source. Install only what you need; skip the rest.

| Binary     | Size (stripped) | Scope |
|---|---|---|
| `tssh`      | ~10 MB | Everything below, plus ECS management (connect/exec/cp/health/top/...) |
| `tssh-k8s`  | ~8 MB  | `ks` (service diag) / `kf` (port-forward) / `logs` / `events` |
| `tssh-net`  | ~8 MB  | `socks` / `fwd` / `run` / `shell` / `vpn` / `browser` |
| `tssh-arms` | ~8 MB  | `arms` (alerts / dash / ds / open / query / trace) |
| `tssh-db`   | ~8 MB  | `redis` / `rds` (built-in RESP and MySQL wire clients) |

All slim binaries honour the same `--profile / -p` flag, read the same
`~/.tssh/config.json`, and share `internal/` packages — so they can't drift
in behaviour from the main `tssh` binary.

```bash
# Build all five binaries locally
make build
sudo cp tssh tssh-k8s tssh-net tssh-arms tssh-db /usr/local/bin/

# Or just the slim ones you want
make tssh-k8s tssh-net
sudo cp tssh-k8s tssh-net /usr/local/bin/
```

### Cross-compile for release

```bash
make all    # Builds all 5 binaries × darwin/linux/windows × amd64/arm64
            # = 30 artifacts in dist/
```

## Usage

### Basic

```bash
# Interactive fuzzy search & connect
tssh

# List all instances
tssh ls
tssh ls -j    # JSON output

# Connect by index, name, or IP
tssh 5
tssh my-production-server

# Sync ECS instance cache
tssh sync
```

### Remote Execution

```bash
# Execute on a single instance
tssh exec my-server uptime

# Execute with complex shell (base64-encoded, no escaping issues)
tssh exec my-server 'echo "hello $(whoami)" | grep hello && echo $HOME'

# Batch execute on matching instances (concurrent)
tssh exec -g "prod-web" uptime

# With progress indicator
tssh exec -g "prod-web" --progress "df -h"

# JSON output (for AI/script consumption)
tssh exec -j my-server hostname
tssh exec -g "prod" -j "free -m"

# Quiet mode (results only, no headers)
tssh exec -g "prod" -q hostname

# Pipe script from stdin
echo 'apt update && apt upgrade -y' | tssh exec my-server -

# Execute script file
tssh exec -s deploy.sh my-server

# Custom timeout
tssh exec --timeout 120 my-server "long-running-task"

# Webhook notification on completion
tssh exec --notify https://hook.example.com/webhook -g "prod" "apt update"

# Default timeout via env var
export TSSH_DEFAULT_TIMEOUT=300
tssh exec -g "prod" "long-task"

# --- Long-running / resumable execution ---

# Fire and forget: print InvokeId, exit immediately (docker build, migrations, ...)
tssh exec --async my-server "docker build -t foo . && docker push foo"
#   t-hyz1xxxxxxxxxxxx    my-server    10.0.0.5

# Fetch output later — works for Running (partial) or Finished invocations
tssh exec --fetch t-hyz1xxxxxxxxxxxx

# Stop a runaway command
tssh exec --stop t-hyz1xxxxxxxxxxxx

# Blocking mode still emits InvokeId on timeout, so you can recover:
tssh exec --timeout 60 my-server "sleep 3600"
# ❌ Error: command timed out after 60s (invoke_id=t-xxx, 用 `tssh exec --fetch t-xxx` 取结果)
tssh exec --fetch t-xxx
```

### Connectivity Test

```bash
# Ping a single instance
tssh ping my-server

# Batch ping
tssh ping -g "prod-web"
```

### Health Inspection

```bash
# Check all instances
tssh health

# Filter by pattern
tssh health -g "gateway"

# Alert-only mode
tssh health -a

# Export as JSON/Markdown/CSV
tssh health -j
tssh health --format md -o report.md
```

### File Transfer

```bash
# Upload a file
tssh cp ./file.txt my-server:/tmp/file.txt

# Download a file
tssh cp my-server:/tmp/file.txt ./file.txt

# Batch upload to multiple instances
tssh cp -g "prod-web" ./config.yaml :/etc/app/config.yaml

# rsync via tunnel
trsync ./dist/ my-server:/var/www/html/
```

### Port Forwarding

```bash
# Tunnel remote port 80 to local 8080
tssh -L 8080:localhost:80 my-server

# Tunnel through remote host (via relay)
tssh -L 8080:internal-db:3306 my-server
```

### Dev "VPN" — access internal services from localhost

Five commands progressively cover the "I'm on my laptop but need to hit prod
VPC" problem without requiring a bastion SSH key or VPN client:

```bash
# 1. One port, zero config. Auto-pick a same-VPC ECS as jump host.
tssh fwd rds-prod.internal:3306         # any host:port
tssh fwd 10.0.0.5:8080                  # any internal IP
tssh fwd rm-2zxxxxxx                    # RDS instance ID (auto-resolve VPC)
tssh fwd r-bpxxxxxx                     # Redis instance ID
#   📡 127.0.0.1:54321  →  rds-prod.internal:3306  (via prod-jump)

# 2. Multiple ports at once + inject env vars into a child process.
#    Perfect for `./gradlew bootRun` with a bunch of dependencies.
tssh run --to mysql=rm-xxx,redis=r-xxx,kafka=10.0.0.3:9092 -- ./gradlew bootRun
#   Child sees: MYSQL_HOST/MYSQL_PORT/MYSQL_ADDR, REDIS_*, KAFKA_*
#   application.yml uses ${MYSQL_HOST:localhost}:${MYSQL_PORT:3306} and "just works"

# 3. Generic SOCKS5 proxy (remote microsocks + port-forward).
tssh socks prod-jump
#   🧦 SOCKS5: 127.0.0.1:1080 (via prod-jump)
#   curl --socks5-hostname 127.0.0.1:1080 https://internal.example

# 4. Subshell with ALL_PROXY / JAVA_TOOL_OPTIONS preset.
#    curl / git / go / JVM HTTP/JDBC all transparent.
tssh shell prod-jump
(prod-jump) $ ./gradlew bootRun     # JDBC/HTTP go through prod-jump
(prod-jump) $ exit                  # auto-cleanup

# 5. Real L3 TUN proxy — Kafka/MQ/gRPC all work (SOCKS can't help those).
#    Needs `go install github.com/xjasonlyu/tun2socks/v2@latest` once.
sudo tssh vpn prod-jump --cidr 10.0.0.0/16
sudo tssh vpn prod-jump --cidr 10.0.0.0/16,172.16.0.0/12
```

| Command | Config? | sudo? | Covers |
|---|---|---|---|
| `tssh fwd` | none | no | one service |
| `tssh run` | `--to key=v` | no | many services, env injection for child |
| `tssh socks` | none | no | SOCKS5 for manual clients |
| `tssh shell` | none | no | JVM/HTTP/CLI, drops you in a subshell |
| `tssh vpn`  | `--cidr` | yes | any protocol (Kafka/MQ/gRPC) via TUN |

All five clean up automatically on Ctrl-C (remote microsocks/socat, local
port-forwards, routes).

### Multi-Account

```bash
# List profiles
tssh profiles

# Use a specific profile
tssh --profile staging ls
tssh -p staging exec my-server uptime
```

### ARMS Monitoring

```bash
# View firing alerts (uses Aliyun credentials, no extra config)
tssh arms

# List Grafana dashboards
tssh arms dash

# Search dashboards
tssh arms dash API

# Open dashboard in browser
tssh arms open Application

# List data sources
tssh arms ds

# Built-in shortcut queries
tssh arms query services               # List all monitored services
tssh arms query errors [service]        # Error count (5min)
tssh arms query latency [service]       # Avg response time
tssh arms query slow-sql [service]      # Slow SQL count
tssh arms query qps [service]           # Requests per second
tssh arms query cpu [service]           # CPU usage
tssh arms query mem [service]           # Memory usage
tssh arms query gc [service]            # Full GC count

# Custom PromQL
tssh arms query 'arms_app_requests_count_raw{service="my-svc"}'

# Distributed Trace — view spans of a specific trace
tssh arms trace 0a1b2c3d4e5f6708091a2b3c4d5e6f70

# Find the trace whose spans carry tag globalId=<value>
# (previously only possible via ARMS console UI)
tssh arms trace --globalId req-abc-123

# Arbitrary custom tags, plus filters
tssh arms trace --tag userId=42 --since 2h --limit 20
tssh arms trace --tag bizCode=ORDER --pid <app-pid>

# JSON output
tssh arms alerts -j
tssh arms query -j errors my-service
tssh arms trace -j 0a1b2c3d...
```

**Configuration:** Alerts (`tssh arms`) require only Aliyun credentials. Dashboard/query commands need Grafana token:
```bash
export TSSH_GRAFANA_URL=https://your-grafana.grafana.aliyuncs.com
export TSSH_GRAFANA_TOKEN=glsa_xxx
```

### Web Management

```bash
# Start web UI
tssh web --port 8080

# With token authentication
tssh web --port 8080 --token my-secret-token
```

## Project Structure

```
cmd/tssh/          # CLI 入口 + 子命令
internal/model/     # 共享类型 (Instance, Config, etc.)
internal/config/    # 多源凭证加载
internal/cache/     # 实例缓存 + 模式匹配
internal/aliyun/    # ECS/ARMS API 客户端 (限流 + 重试)
internal/grafana/   # Grafana HTTP API 客户端
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ALIBABA_CLOUD_ACCESS_KEY_ID` | Aliyun AccessKey ID |
| `ALIBABA_CLOUD_ACCESS_KEY_SECRET` | Aliyun AccessKey Secret |
| `ALIBABA_CLOUD_REGION_ID` | Region (default: cn-beijing) |
| `TSSH_DEFAULT_TIMEOUT` | Default exec timeout in seconds (default: 60) |
| `TSSH_GRAFANA_URL` | Grafana endpoint for ARMS monitoring |
| `TSSH_GRAFANA_TOKEN` | Grafana Service Account token |

## Performance

| Scenario | Details |
|----------|--------|
| Single instance exec | ~1.4s |
| 16 instances batch | ~2.8s |
| 200 instances batch | Rate-limited, auto-retry on throttle |

## Security Model

`tssh` uses `StartTerminalSession` and `RunCommand` APIs. Ensure your RAM Key has minimum permissions:
- `ecs:StartTerminalSession`
- `ecs:DescribeInstances`
- `ecs:RunCommand`
- `ecs:DescribeInvocationResults`
- `ecs:SendFile`

## License

MIT License
