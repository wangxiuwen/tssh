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
- [x] `internal/cmd/k8s/logs.go` + test (v1.16.0-refactor.5)
- [x] `internal/cmd/k8s/ks.go` + test (v1.16.0-refactor.5)
- [x] shared.DefaultStr 抽离 (ks + kf 共用)
- [x] `internal/cmd/k8s/kf.go` + test (v1.16.0-refactor.6)
- [x] core.Runtime.StartPortForward + StartSocatRelay (v1.16.0-refactor.6)
- [x] **`cmd/tssh-k8s/main.go` 独立 binary 编出来** (4.4MB, 主 tssh 14.4MB,
      69% 缩减). 但 runtime 还是 stub — Phase 3 才能真跑.
- [ ] `internal/cmd/net/` (socks/fwd/run/shell/vpn/browser)
- [ ] `internal/cmd/db/` (redis/rds)
- [ ] `internal/cmd/arms/`
- [ ] `internal/cmd/core/` (剩下的 ECS 基础能力)

### Phase 3 — 拆 main

- [x] `internal/runtime.Runtime` 共享实现 (config / cache / ResolveInstance
      非 TUI 版 / ExecOneShot). 可注入 ExecInteractiveFn /
      StartPortForwardFn / StartSocatRelayFn 给有能力的 binary 用
- [x] cmd/tssh/runtime.go 改为懒代理, 注入主 tssh 特有的 hook
      (ConnectSessionWithCommand / startPortForwardBgWithCancel / setupSocatRelay)
- [x] cmd/tssh-k8s 真能跑 events/ks/logs (v1.16.0-refactor.7)
      kf 需要 port-forward hook — 下一轮把 session/portforward/socat 挪到
      internal 后 tssh-k8s 就能全功能
- [x] Makefile 支持 `BINARIES = tssh tssh-k8s`, `make all` 自动 2×6 = 12
      个 cross-compiled 产物 (v1.16.0-refactor.8)
- [x] release.yml 零改动 (make all 产物已全部上传)
- [ ] 后续新 binary (tssh-net / tssh-db / tssh-arms) 只要加到 BINARIES 即可

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
