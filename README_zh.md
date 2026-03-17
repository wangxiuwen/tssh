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
- **免端口文件传输：** 基于 SendFile API + SCP 大文件支持 (`tssh cp`)。
- **端口映射：** 支持远程主机中转 (`tssh -L 8080:remote:3306 <name>`)。
- **API 限流保护：** 内置速率限制器，200+ 台机器批量操作自动退避重试。
- **Shell 补全：** Bash/Zsh 补全支持 (`tssh completion`)。
- **执行历史：** 查看过去的执行记录 (`tssh history`)。
- **缓存自动刷新：** 超过 24 小时自动后台静默刷新。

## 凭证配置

需要 RAM 用户的 API AccessKey 配置。工具会按以下优先级自动寻找凭证：
1. 环境变量 (`ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_REGION_ID`)
2. tssh 多账号配置 (`~/.tssh/config.json`)
3. 阿里云官方 CLI 的配置缓存 (`~/.aliyun/config.json`)

## 快速安装

从 [GitHub Releases](https://github.com/wangxiuwen/tssh/releases) 下载预编译二进制，或从源码编译：

```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
make build
sudo cp tssh /usr/local/bin/
```

一键交叉编译所有平台：
```bash
make all    # 编译 darwin/linux/windows × amd64/arm64
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
