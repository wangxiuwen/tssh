# tssh (基于阿里云原生 API 的终端连接工具)

`tssh` 是一个开源的、无需外部依赖的单二进制命令行工具，旨在优雅地解决阿里云 ECS 实例的管理与直连痛点。

**核心痛点解决：** 当你需要管理成百上千台**没有公网 IP** 的内网机器时，传统的登录方式（寻找内网 IP、繁琐地挂载跳板机/Jumpserver、配置和分发 SSH Key）会变得极其低效和令人沮丧。`tssh` 彻底打破了这一桎梏。它直接通过阿里云官方的 Cloud Assistant（云助手）和 Cloud Shell WebSocket 协议实现底层终端复用。全程无需 22 端口暴露，一键即可安全直达目标内核。

### 🤖 AI Agent 友好设计

`tssh` 的 v1.1.0 版本专门为 AI Agent 和自动化管线优化：
- **JSON 结构化输出** (`-j`/`--json`) — 所有命令均支持，可直接 `jq` 管道处理
- **stdin 管道输入** — 多行脚本直接喂入：`echo 'script' | tssh exec name -`
- **脚本文件执行** — `tssh exec -s deploy.sh name`
- **退出码透传** — 远程命令退出码直接传递到 `$?`
- **安静模式** (`-q`) — 去掉所有修饰，只输出纯结果
- **高性能批量** — 并发执行 + API 限流保护 + 自动重试

[English Documentation](README.md)

## 功能特性

- **交互式真终端：** 一键开启带有 PTY 的完整 WebSocket Pseudo-Terminal。
- **实时模糊搜索：** 毫秒级 FZF 风格终端内搜索，支持通过实例名、ID 或 IP 过滤。
- **快捷序号登录：** 直接使用列表序号登录（如 `tssh 5`）。
- **远程批量执行：** 并发下发脚本到多台机器 (`-g`)，支持进度显示、标签过滤、JSON 输出。
- **连通性测试：** 通过云助手快速测试实例连接 (`tssh ping`)。
- **深度健康巡检：** CPU、内存、磁盘、JVM、OOM、TIME_WAIT 异常检测 (`tssh health`)。
- **多账号管理：** `~/.tssh/config.json` 多账号配置 (`--profile`)。
- **SSH 兼容参数：** 支持标准 SSH 参数 (`-l`, `-p`, `-i`, `-o` 等)，无缝替换 ssh。
- **JSON 结构化输出：** 所有命令支持 `-j` 输出。
- **stdin / 脚本输入：** 支持管道输入和脚本文件 (`-s`)。
- **退出码透传：** 远程命令退出码自动传递到本地进程。
- **免端口文件传输：** SendFile API 分块传输任意大小文件 (`tssh cp`)，数百 MB 可走 OSS 中转 (`--bucket`)。
- **端口映射：** 支持远程主机中转 (`tssh -L 8080:remote:3306 <name>`)。
- **研发"临时 VPN"全家桶：** 本机访问 VPC 内网, 不用 SSH key/VPN 客户端 — `tssh fwd` (单端口零配置), `tssh run` (多端口 + env 注入, Spring 场景), `tssh socks` (SOCKS5 代理), `tssh shell` (子 shell 预置代理环境变量), `tssh vpn` (L3 TUN 透明代理, Kafka/MQ/gRPC 都吃).
- **API 限流保护：** 内置速率限制器，200+ 台机器批量操作自动退避重试。
- **Shell 补全：** Bash/Zsh 补全支持 (`tssh completion`)。
- **执行历史：** 查看过去的执行记录 (`tssh history`)。
- **ARMS 监控集成：** 一键查看告警、仪表盘管理、Prometheus 快捷查询、按 `TraceID` 或 `globalId` / 自定义 tag 查分布式 trace (`tssh arms`)。
- **缓存自动刷新：** 超过 24 小时自动后台静默刷新。

## 凭证配置

需要 RAM 用户的 API AccessKey 配置。工具会按以下优先级自动寻找凭证：
1. 环境变量 (`ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_REGION_ID`; STS 场景可加 `ALIBABA_CLOUD_SECURITY_TOKEN`)
2. tssh 多账号配置 (`~/.tssh/config.json`)
3. 阿里云官方 CLI 的配置缓存 (`~/.aliyun/config.json`) — 支持静态 `AK` 和 `CloudSSO` / STS 两种 profile (`sts_token` 会自动透传)；STS 过期时执行 `aliyun sso login --profile <name>` 刷新。

## 快速安装

### 方式一 — 全家桶

从 [GitHub Releases](https://github.com/wangxiuwen/tssh/releases) 下载预编译二进制，或从源码编译：

```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
make build
sudo cp tssh /usr/local/bin/
```

### 方式二 — 按维度装小 binary

tssh 从同一套源码构建出一组按职责拆分的小二进制, 只装你需要的, 不带冗余依赖.

| Binary     | 体积 (stripped) | 范围 |
|---|---|---|
| `tssh`      | ~10 MB | 下方所有能力 + ECS 管理 (connect/exec/cp/health/top/...) |
| `tssh-k8s`  | ~8 MB  | `ks` (svc 诊断) / `kf` (端口转发) / `logs` / `events` |
| `tssh-net`  | ~8 MB  | `socks` / `fwd` / `run` / `shell` / `vpn` / `browser` |
| `tssh-arms` | ~8 MB  | `arms` (alerts / dash / ds / open / query / trace) |
| `tssh-db`   | ~8 MB  | `redis` / `rds` (内置 RESP + MySQL wire 客户端) |

所有小 binary:
- 相同的 `--profile / -p` 语义, 共用 `~/.tssh/config.json`
- 共用 `internal/` 代码, 不会和主 `tssh` 行为漂移
- 子命令 CLI 与主 `tssh` 一致

```bash
# 本机编全部
make build
sudo cp tssh tssh-k8s tssh-net tssh-arms tssh-db /usr/local/bin/

# 只编要的
make tssh-k8s tssh-net
sudo cp tssh-k8s tssh-net /usr/local/bin/
```

### 跨平台交叉编译

```bash
make all    # 5 个 binary × darwin/linux/windows × amd64/arm64
            # = 30 个产物在 dist/
```

## 使用说明

### 基础操作

```bash
# 不带参数：打开实时下拉菜单模糊选择
tssh

# 列出所有缓存实例
tssh ls
tssh ls -j    # JSON 输出

# 根据编号、名称或 IP 直连
tssh 5
tssh my-production-server

# 同步阿里云全量 ECS 列表到本地缓存
tssh sync
```

### 远程执行

```bash
# 单台执行
tssh exec my-server uptime

# 复杂 Shell 命令（base64 编码，无需转义）
tssh exec my-server 'echo "hello $(whoami)" | grep hello && echo $HOME'

# 批量执行（并发）
tssh exec -g "prod-web" uptime

# 带进度指示器
tssh exec -g "prod-web" --progress "df -h"

# JSON 输出（给 AI/脚本消费）
tssh exec -j my-server hostname
tssh exec -g "prod" -j "free -m"

# 安静模式（只输出结果）
tssh exec -g "prod" -q hostname

# 通过 stdin 管道喂入脚本
echo 'apt update && apt upgrade -y' | tssh exec my-server -

# 执行脚本文件
tssh exec -s deploy.sh my-server

# 自定义超时
tssh exec --timeout 120 my-server "long-running-task"

# --- 长任务异步/恢复 ---

# 提交即返回 InvokeId, 本地立刻退出（适合 docker build / 数据迁移等长任务）
tssh exec --async my-server "docker build -t foo . && docker push foo"
#   t-hyz1xxxxxxxxxxxx    my-server    10.0.0.5

# 稍后拉取结果 — Running 状态也能看到已产生的部分输出
tssh exec --fetch t-hyz1xxxxxxxxxxxx

# 强停跑飞的命令
tssh exec --stop t-hyz1xxxxxxxxxxxx

# 阻塞模式超时时也会打印 InvokeId, 可继续 --fetch 取结果
tssh exec --timeout 60 my-server "sleep 3600"
# ❌ Error: command timed out after 60s (invoke_id=t-xxx, 用 `tssh exec --fetch t-xxx` 取结果)
tssh exec --fetch t-xxx
```

### 连通性测试

```bash
# 单台 ping
tssh ping my-server

# 批量 ping
tssh ping -g "prod-web"
```

### 健康巡检

```bash
# 全量巡检
tssh health

# 按模式过滤
tssh health -g "gateway"

# 只看告警
tssh health -a

# 导出报告
tssh health -j
tssh health --format md -o report.md
```

### 文件传输

```bash
# 上传文件
tssh cp ./file.txt my-server:/tmp/file.txt

# 下载文件
tssh cp my-server:/tmp/file.txt ./file.txt

# 批量上传到多台实例
tssh cp -g "prod-web" ./config.yaml :/etc/app/config.yaml

# 数百 MB 走 OSS 中转 (本地需 ossutil; 远端只需 curl)
tssh cp --bucket devops-turing ./big.tar.gz my-server:/tmp/big.tar.gz
tssh cp --bucket devops-turing my-server:/var/log/big.log ./big.log

# rsync 同步
trsync ./dist/ my-server:/var/www/html/
```

### 端口映射

```bash
# 将远端 80 映射到本地 8080
tssh -L 8080:localhost:80 my-server

# 通过远程主机中转
tssh -L 8080:internal-db:3306 my-server
```

### 研发"临时 VPN" — 从本机访问 VPC 内网服务

5 条命令覆盖 "我在笔记本上, 需要连到 prod VPC" 的全部场景, 不用 SSH key、
不用 VPN 客户端:

```bash
# 1. 单端口, 零配置. tssh 自动挑同 VPC 的 ECS 做跳板.
tssh fwd rds-prod.internal:3306      # 任意 host:port
tssh fwd 10.0.0.5:8080               # 任意内网 IP
tssh fwd rm-2zxxxxxx                 # RDS 实例 ID (自动查 VPC)
tssh fwd r-bpxxxxxx                  # Redis 实例 ID
#   📡 127.0.0.1:54321 → rds-prod.internal:3306 (via prod-jump)

# 2. 一次多端口 + 把环境变量注入子进程 — Spring 场景最常用.
tssh run --to mysql=rm-xxx,redis=r-xxx,kafka=10.0.0.3:9092 -- ./gradlew bootRun
#   子进程 env:  MYSQL_HOST/PORT/ADDR, REDIS_*, KAFKA_*
#   application.yml 用 ${MYSQL_HOST:localhost}:${MYSQL_PORT:3306} 即可

# 3. 通用 SOCKS5 代理 (远端起 microsocks, 本地转发).
tssh socks prod-jump
#   🧦 SOCKS5: 127.0.0.1:1080 (via prod-jump)
#   curl --socks5-hostname 127.0.0.1:1080 https://internal.example

# 4. 起一个子 shell, 预置 ALL_PROXY / JAVA_TOOL_OPTIONS.
#    curl / git / go / JVM 的 HTTP 和 JDBC 透明走远端.
tssh shell prod-jump
(prod-jump) $ ./gradlew bootRun      # JDBC/HTTP 走 prod-jump
(prod-jump) $ exit                   # 自动清理

# 5. L3 TUN 透明代理 — Kafka/MQ/gRPC 等 SOCKS 搞不定的协议.
#    需要一次性装: go install github.com/xjasonlyu/tun2socks/v2@latest
sudo tssh vpn prod-jump --cidr 10.0.0.0/16
sudo tssh vpn prod-jump --cidr 10.0.0.0/16,172.16.0.0/12
```

| 命令 | 配置 | sudo | 适用 |
|---|---|---|---|
| `tssh fwd` | 无 | 否 | 单服务 |
| `tssh run` | `--to key=v` | 否 | 多服务 + 子进程 env 注入 |
| `tssh socks` | 无 | 否 | 自己的客户端手动配代理 |
| `tssh shell` | 无 | 否 | JVM/HTTP/CLI, 进子 shell 直接用 |
| `tssh vpn`  | `--cidr` | 是 | 任意协议 (Kafka/MQ/gRPC), TUN 层 |

Ctrl-C 都会反向清理 (远端 microsocks/socat、本地转发、路由).

### 多账号

```bash
# 列出账号
tssh profiles

# 使用指定账号
tssh --profile staging ls
tssh -p staging exec my-server uptime
```

## 性能基准

| 场景 | 详情 |
|------|------|
| 单台执行 | ~1.4s |
| 16 台执行 | ~2.8s |
| 200 台批量 | 自动限流 + 退避重试 |

## 安全性建议

请赋予 RAM Key 最小权限：
- `ecs:StartTerminalSession`
- `ecs:DescribeInstances`
- `ecs:RunCommand`
- `ecs:DescribeInvocationResults`
- `ecs:SendFile`

## License

MIT License
