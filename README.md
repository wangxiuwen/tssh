# tssh (Terminal SSH for Aliyun)

`tssh` is an open-source, zero-dependency, single-binary CLI tool designed to solve the massive headache of managing Alibaba Cloud (Aliyun) ECS instances natively. 

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
- **Self-Diagnostics:** Check credentials, API connectivity, cache, dependencies (`tssh doctor`).
- **Self-Update:** Automatic update from GitHub Releases (`tssh update`).
- **Remote Log Tailing:** Follow remote logs in real-time (`tssh tail`).
- **Periodic Monitoring:** Watch command output with auto-refresh (`tssh watch`).
- **Live Dashboard:** Real-time instance monitoring panel (`tssh top`).
- **Multi-Instance Diff:** Compare command output across machines with color diff (`tssh diff`).
- **Instance Lifecycle:** Stop, start, reboot instances with status polling (`tssh stop/start/reboot`).
- **Persistent Tunnels:** Manage long-running port forwarding tunnels (`tssh tunnel start/list/stop`).
- **Web Management UI:** Embedded dark-themed web dashboard with search and remote exec (`tssh web`).
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
- **rsync Support:** Native rsync via tunnel (`trsync`).
- **API Rate Limiting:** Built-in rate limiter and automatic retry on API throttling.
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

Download pre-built binaries from [GitHub Releases](https://github.com/wangxiuwen/tssh/releases), or compile from source:

```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
make build
sudo cp tssh /usr/local/bin/
```

Cross-compile for all platforms:
```bash
make all    # Builds darwin/linux/windows × amd64/arm64
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

### Multi-Account

```bash
# List profiles
tssh profiles

# Use a specific profile
tssh --profile staging ls
tssh -p staging exec my-server uptime
```

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
