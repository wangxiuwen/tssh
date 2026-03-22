---
description: 如何处理 GitHub Issues（查看、分析、修复、回复、关闭）
---
# Issue 处理工作流

## 1. 获取所有 Open Issues

// turbo
```bash
gh issue list --state open --limit 50 --json number,title,body,labels,state,author,createdAt
```

## 2. 分析每个 Issue

对每个 issue 进行分类：

| 类型 | 处理方式 |
|------|----------|
| **合理 Bug** | 定位代码 → 修复 → 写测试 → 回复并关闭 |
| **合理 Feature** | 评估合理性 → 实现 → 写测试 → 回复并关闭 |
| **不合理 / 非 Bug** | 回复解释原因 → 关闭（reason: `not planned`） |
| **重复 Issue** | 回复指向原 issue → 关闭（reason: `not planned`） |
| **已修复** | 确认代码已包含修复 → 回复说明 → 关闭（reason: `completed`） |

**识别重复的要点：**
- 同一作者在不同时间提交的相同问题
- 描述的根因相同但表现不同（如 PTY 宽度问题可能表现为多个不同命令的输出错乱）

## 3. 代码修复流程

对于合理的 Bug / Feature：

1. **定位相关代码** — 用 `grep_search` 和 `view_file` 找到相关源码
2. **理解上下文** — 阅读相关函数、调用链、测试文件
3. **实施修复** — 修改源码
4. **更新测试** — 确保现有测试适配，添.新测试覆盖修复点
5. **编译验证**：

// turbo
```bash
go build ./cmd/tssh
```

6. **运行测试**：

// turbo
```bash
go test ./...
```

## 4. 回复并关闭 Issue

### 合理 Bug（已修复）

```bash
gh issue comment <NUMBER> --body "已修复。

**根因：** <简要描述根因>

**修改内容：**
- <文件/函数>: <修改摘要>

**示例：**
\`\`\`
<使用示例>
\`\`\`"
gh issue close <NUMBER> --reason completed
```

### 不合理 / 非 Bug

```bash
gh issue comment <NUMBER> --body "关闭此 issue。

**原因说明：** <解释为什么这不是 bug>

**Workaround：**
- <可用的替代方案>"
gh issue close <NUMBER> --reason "not planned"
```

### 重复 Issue

```bash
gh issue comment <NUMBER> --body "关闭此 issue，与 #<ORIGINAL> 重复。已在 #<ORIGINAL> 中处理。"
gh issue close <NUMBER> --reason "not planned"
```

## 5. 注意事项

- **先分析再动手** — 读完所有 issue 后统一规划，避免重复工作
- **信息输出到 stderr** — 所有提示/警告信息用 `fmt.Fprintln(os.Stderr, ...)` 输出，保持 stdout 干净以便管道处理
- **跨平台兼容性** — 修改终端/信号相关代码时注意 Windows 兼容（build tags）
- **回复用中文** — issue 和 README 均为中文，回复保持中文
- **先构建再发布** — 所有代码修改后必须通过 `go build` 和 `go test`
