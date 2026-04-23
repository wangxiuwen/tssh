# tssh 拆多 binary 重构蓝图

**目标**: 从 "一个 30+ 子命令的大杂烩 `tssh`" 拆成多个小 binary, 每个专注一个维度.
向后兼容: 主 `tssh` 继续全量, 新增的小 binary 是子集.

## 最终形态

| Binary | 子命令 | 定位 |
|---|---|---|
| `tssh` (core) | connect / ls / sync / exec / cp / health / ping / info / tail / watch / diff / stop / start / reboot / top / tunnel / web / doctor / update / ssh-config / profiles / history | 原有 ECS 管理核心 |
| `tssh-net` | fwd / run / socks / shell / vpn / browser | 本机接入内网 |
| `tssh-k8s` | ks / kf / logs / events | k8s 开发辅助 |
| `tssh-db` | redis / rds | 数据库 REPL |
| `tssh-arms` | arms (alerts/dash/ds/open/query/trace) | 可观测性 |

所有小 binary 共享: 配置 / 缓存 / Aliyun 客户端 / shared 工具函数.

## 分包

```
internal/
  shared/         纯函数: ShellQuote, FindFreePort, parseTimeoutSec, decodeOutput, ...
  aliyun/         SDK 封装 (已有)
  cache/          instances.json 读写 (已有)
  config/         credentials 加载 (已有)
  grafana/        (已有)
  model/          (已有)
  cmd/
    core/         cmdList/cmdExec/cmdCopy/... (挪自 cmd/tssh/*)
    net/          cmdFwd/cmdRun/cmdSocks/cmdShell/cmdVPN/cmdBrowser
    k8s/          cmdKS/cmdKF/cmdLogs/cmdEvents
    db/           cmdRedis/cmdRDS
    arms/         cmdArms

cmd/
  tssh/           大杂烩 main, 注册所有 dispatch (向后兼容)
  tssh-net/       只 dispatch net 子集
  tssh-k8s/       只 dispatch k8s 子集
  tssh-db/        只 dispatch db 子集
  tssh-arms/      只 dispatch arms 子集
```

## 分阶段计划 (每轮 loop 推进一步)

### Phase 1 — 共用辅助抽离 ✓ 完成
- [x] 建 `internal/shared/`
- [x] ShellQuote (v1.16.0-refactor.1)
- [x] FindFreePort / FindFreePortInRange (v1.16.0-refactor.2)
- [x] ParseTimeoutSec (v1.16.0-refactor.2)
- [x] DecodeOutput / TruncateStr / IsTerminal / FileExists (v1.16.0-refactor.2)
- [x] Fatal / FatalMsg (v1.16.0-refactor.3)
- [x] SleepDuration / SleepMs / ExecCommand (v1.16.0-refactor.3)
- [x] 建 `internal/core/` + Runtime 契约 (v1.16.0-refactor.3)

原则: cmd/tssh 保留 wrapper delegate 到 shared, 向后兼容.

### Phase 2 — 子命令挪到 internal/cmd/<group>

每组包含: 子命令入口 `CmdXXX(rt core.Runtime, args)` / 组内 helper / 各自 `_test.go`.
组只依赖 `internal/shared` 和 `internal/core`, 不依赖 cmd/tssh.

迁移进度:
- [x] `internal/cmd/k8s/events.go` + test (v1.16.0-refactor.4, PoC)
  - cmd/tssh/events.go 变成 6 行 delegate 到 k8s.Events(appRuntime, args)
- [ ] `internal/cmd/k8s/ks.go` + kf.go + logs.go
- [ ] `internal/cmd/net/` (socks/fwd/run/shell/vpn/browser)
- [ ] `internal/cmd/db/` (redis/rds)
- [ ] `internal/cmd/arms/`
- [ ] `internal/cmd/core/` (剩下的 ECS 基础能力)

### Phase 3 — 拆 main
每组写独立 `cmd/tssh-<group>/main.go`, 只注册该组的 dispatch.
主 `cmd/tssh/main.go` 继续注册全部 (向后兼容).

Makefile 加 targets: `make tssh-net`, `make tssh-k8s`, ...
Release workflow 产物加各 platform × 各 binary.

### Phase 4 — 文档 & 安装建议
- README 分节: 有洁癖只装对应 binary, 懒就装 tssh 全家桶
- 更新安装指南, brew tap 或 GitHub release 分别提供

## 不拆的原因

- `internal/aliyun` 不拆 — SDK 层就一个, 没意义
- `internal/config` 不拆 — 配置加载跨所有 binary 共享
- `tssh` 大杂烩不移除 — 很多人可能只装这一个, 保持兼容

## 对现有功能的影响

重构过程中:
- 每一轮都保证 `go build ./...` + `go test ./...` 通过
- 主 `tssh` 二进制的子命令一直可用
- 不改任何子命令的 CLI 语义

完成后 (多轮之后):
- 主 `tssh` 二进制大小下降 ? 实际上不变 (它注册全部)
- 小 binary 大小显著缩小 (~减少 60-70%)
- 依赖 (比如 Cloud SDK) 仍共享, 所有 binary 都带
