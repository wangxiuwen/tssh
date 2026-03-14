# tssh (Terminal SSH for Aliyun)

`tssh` is an open-source, zero-dependency, single-binary CLI tool designed to solve the massive headache of managing Alibaba Cloud (Aliyun) ECS instances natively. 

When managing a fleet of hundreds of servers without public IP addresses, the traditional workflow of hunting down internal IPs, proxying through Bastion hosts (Jump Servers), and distributing SSH keys becomes incredibly cumbersome and inefficient. `tssh` eliminates all of this overhead. By natively leveraging Aliyun's Cloud Assistant and Cloud Shell WebSocket APIs, it establishes secure tunnels directly to your instances using only your Aliyun API credentials.

### 🤖 AI Agent Friendly
Beyond its interactive UI, `tssh` is built with a highly scriptable architecture. Its structured CLI parameter design, regex-based bulk execution capabilities, and deterministic base64-decoded synchronous outputs make it the perfect tool to feed directly to **AI Agents** for automated infrastructure operations and context ingestion.

[中文文档 (Chinese)](README_zh.md)

## Features

- **Interactive Terminal:** Start a full WebSocket-based pseudo-terminal inside any instance with one command.
- **Interactive Search:** FZF-like real-time searching by instance Name, ID, or IP address (`tssh`).
- **Index-based Login:** Fast-connect via numerical index (`tssh 5`).
- **Remote Execution:** Run commands asynchronously on single or multiple instances (matched by keyword regex) and receive base64-decoded synchronous output.
- **File Transfer:** Send files natively via Cloud Assistant API (`tssh cp`).
- **Port Forwarding:** Create ad-hoc local port tunnels mimicking ssh (`tssh -L 8080:localhost:80 <name>`).
- **rsync Support via Tunnel:** Bind `rsync` traffic natively through the Aliyun tunnel over a local proxy using symlink extensions (`trsync`).

## Prerequisites

Aliyun RAM AccessKeys. It searches automatically in the following precedence:
1. Environment variables (`ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_REGION_ID`)
2. Aliyun standard config (`~/.aliyun/config.json`)

## Installation

You can compile it using Go directly:
```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
go build -o tssh .
sudo cp tssh /usr/local/bin/
```

Or just use the `Makefile`:
```bash
make all
```

## Usage

```bash
# Enter interactive UI to select instances
tssh

# List all instances and their numeric IDs
tssh ls

# Connect by index, name, or IP
tssh 5
tssh my-production-server
tssh 192.168.0.12

# Sync the local cache manually (fetches fresh ECS list from Aliyun)
tssh sync

# Send a local file to a remote instance
tssh cp ./file.txt my-server:/tmp/file.txt

# Execute a command on a group of servers (regex matched)
tssh exec -g "prod-web" "uptime"

# Tunnel remote port 80 to your local port 8080
tssh -L 8080:localhost:80 my-server
```

## Security Model

Because `tssh` uses the `StartTerminalSession` and `RunCommand` APIs heavily, please ensure your RAM Key has the tightest permissions available for `ecs:StartTerminalSession`, `ecs:DescribeInstances`, and `ecs:RunCommand`.

## License

MIT License
