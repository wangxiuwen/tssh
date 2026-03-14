# tssh (基于阿里云原生 API 的终端连接工具)

`tssh` 是一个开源的、无需外部依赖的单二进制命令行工具，专为阿里云 ECS 实例设计。它直接通过阿里云官方的 Cloud Assistant（云助手）和 Cloud Shell WebSocket 协议实现终端复用，完全不需要机器拥有公网 IP，也无需配置跳板机 (Jumpserver) 以及复杂的 SSH Key 免密登录体系。

[English Documentation](README.md)

## 功能特性

- **交互式真终端：** 一键开启带有 PTY 的完整 WebSocket Pseudo-Terminal（防中文乱码、支持命令高亮和 Ctrl+C）。
- **实时模糊搜索：** 直接执行 `tssh` 将开启 FZF 风格的毫秒级终端内搜索，支持通过实例名、ID 或内外网 IP 过滤。
- **快捷序号登录：** 支持 `tssh ls` 查看缓存后，直接使用列表序号登录（如 `tssh 5`）。
- **远程批量执行：** 通过云助手原生接口下发脚本 (`tssh exec`)，支持使用正则 (`-g`) 并发下发给多台机器，并直接返回 decoded 输出。
- **免端口文件传输：** 基于 SendFile API 实现纯内网、无 22 端口的小文件跨网段直接分发 (`tssh cp`)。
- **纯原生端口映射：** 类似于 `ssh -L` 的功能，将远端服务器端口安全隧道透传到本地 (`tssh -L 8080:localhost:80 <name>`)。
- **高级扩展指令：** 通过软连接特性，自动分发为 `tscp` (调用系统 scp) 和 `trsync` (自动打洞隧道复用 rsync，解决大文件同步限额)。

## 凭证配置

需要 RAM 用户的 API AccessKey 配置。工具会按以下优先级自动寻找凭证：
1. 环境变量 (`ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_REGION_ID`)
2. 阿里云官方 CLI 的配置缓存 (`~/.aliyun/config.json`)

## 快速安装

直接使用 Go 原生编译：
```bash
git clone https://github.com/wangxiuwen/tssh.git
cd tssh
go build -o tssh .
sudo cp tssh /usr/local/bin/
```
或者使用随附的 Makefile 一键交叉编译所有平台：
```bash
make all
```

## 使用说明

```bash
# 不带参数：打开实时下拉菜单模糊选择（支持输 IP、机器名的一段）
tssh

# 列出所有缓存实例及其编号
tssh ls

# 根据缓存编号、名称或确切的 IP 直连
tssh 5
tssh my-production-server
tssh 192.168.0.12

# 同步阿里云全量 ECS 列表到本地缓存
tssh sync

# 将本地文件通过原生云助手协议送进远端任意目录
tssh cp ./file.txt my-server:/tmp/file.txt

# 根据正则匹配同时在多台机器上并发执行 shell
tssh exec -g "prod-web" "uptime"

# 打原生隧道，将目标机器内网 80 端口映射到本地的 8080
tssh -L 8080:localhost:80 my-server
```

## 安全性建议

由于 `tssh` 强依赖于云助手与会话管理，请赋予执行的 RAM Key 最小权限：`ecs:StartTerminalSession`, `ecs:DescribeInstances`, `ecs:RunCommand`, `ecs:SendFile`。不建议使用主账号凭证。

## License

MIT License
