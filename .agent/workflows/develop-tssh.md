---
description: 如何进行本项目 (tssh) 的后续研发与功能扩展
---
# tssh 项目 AI Agent 研发规范与指引

`tssh` 是一个专门为阿里云 ECS 打造的零依赖终端连接与运维工具。为了保持该项目在未来的迭代中继续保持“零依赖、单二进制、 Agent 友好”的特性，后续协助开发的 AI Agent 需要严格遵循以下几点指南：

## 1. 核心架构前提
- **无公网交互**：工具的底层逻辑是通过阿里云原生的 **Cloud Assistant (云助手)** 和 **Cloud Shell WebSocket** 来解决私有网络服务器访问痛点的。我们**坚决不引入**常规的 SSH 22 端口探测、密钥协商等传统的运维思维。
- **单二进制与零依赖**：`tssh` 强调极致的简洁部署。所有的功能（如模糊搜索、终端渲染）都已经打包在一个 Go Binary 中。在引入任何第三方模块（`go.mod`）前，请审慎评估其必要性。

## 2. PTY 与 WebSocket 处理机制
- 核心终端会话由 `StartTerminalSession` 生成一次性的 WebSocket URL。由于涉及到 PTY 的窗口大小 (Window Size)，对 `SIGWINCH` 终端信号的处理务必注意跨平台兼容性。（例如，Windows 上没有 `syscall.SIGWINCH`，在修改这部分代码时必须通过 Go build tags 如 `//go:build !windows` 分离开来，详见 `session.go` 和 `resize_unix.go`/`resize_windows.go`）。

## 3. Makefile 与跨平台编译
- 项目已内置 `make all` 跨平台打包逻辑。
- 无论增加任何代码或者修改引用包，请务必执行 `make all` 确保在 MacOS (darwin) 和 Linux 的 AMD64 / ARM64 架构上都能顺利通过编译过程。
- 如果涉及针对某个平台的特殊 API（如终端交互、信号捕捉），必须提供 Dummy 实现应对其它操作系统。

## 4. UI 交互与“输出截获”原则
- 我们在 `ui.go` 里引用了 `manifoldco/promptui` 进行实时模糊查询。当加入新的交互指令时，请注意**对后续自动化执行的友好性**！
- 如果某条命令是带有子参数的（如 `tssh exec -g "xxxx"`），Agent 以及终端外壳希望拿到的是 **最原始的基础结构化字符串（最好以标准输出为主）**，从而便于解析和进行流水线处理。不要画蛇添足地输出无意义的 ANSI 颜色日志到 `stdout`。

## 5. 工作流提交流程
1. **理解意图**：仔细梳理用户是想增加交互 UI、是新增底层 API，还是优化缓存。
2. **实施开发**：严格修改 `main.go`, `aliyun.go`, `session.go`, `ui.go` 这几个核心入口。
3. **本地编译校验**：不要直接推送，务必在本地先执行 `make build` 确保没有依赖报错。
4. **触发 Release**：项目已经接入了 GitHub Actions。开发完毕后，你需要给你的修改打上遵循 SemVer 规范的新 Tag (如 `v1.0.3`)，并推送 Tag 触发线上云打包流程。

// turbo-all
```bash
go mod tidy
make build
```
