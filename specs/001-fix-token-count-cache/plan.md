# Implementation Plan: 修复 Token 计数遗漏缓存命中 Token

**Branch**: `master` | **Date**: 2026-06-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-fix-token-count-cache/spec.md`

## Summary

修复总 Token 计数逻辑：当前 `extractInputOutputTokens` 函数仅提取 `input_tokens`（或 `prompt_tokens`）+ `output_tokens`，遗漏了 Anthropic Prompt Caching 机制返回的 `cache_creation_input_tokens` 和 `cache_read_input_tokens`。修改此函数将缓存 token 合并到返回的输入 token 中，从而同时修复流式和非流式两条路径。

## Technical Context

**Language/Version**: Go 1.24+

**Primary Dependencies**: 标准库 `encoding/json`，内部 `tokencount` 包

**Storage**: SQLite（通过 `storage.StatsStorage` 接口），修改对存储层透明——`inputTokens` 在传递给存储层之前已完成合并

**Testing**: `go test -v ./internal/proxy/...`

**Target Platform**: Linux/macOS/Windows 服务器和桌面

**Project Type**: HTTP 代理服务 + 桌面应用

**Performance Goals**: 无变化——仅在 token 提取时增加两次 map lookup

**Constraints**: 必须向后兼容（无缓存字段时行为不变）

**Scale/Scope**: 修改范围：1 个核心函数 + 测试文件

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Constitution 模板为空（未配置项目原则），无需门控检查。项目遵循 `CLAUDE.md` 中的代码规范：
- 静态函数使用 `__` 前缀（不适用——修改的函数已是公开/包内函数）
- 变量在函数体开头声明并初始化（遵循）

## Project Structure

### Documentation (this feature)

```text
specs/001-fix-token-count-cache/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
└── contracts/           # Phase 1 output (N/A - no external interface changes)
```

### Source Code (repository root)

```
internal/
├── proxy/
│   ├── response.go       # MODIFY: extractInputOutputTokens - add cache token parsing
│   ├── streaming.go       # UNCHANGED (uses extractInputOutputTokens internally)
│   ├── proxy.go           # UNCHANGED (receives already-merged inputTokens)
│   ├── utils.go           # MODIFY: estimateTokens - add cache token estimation fallback
│   ├── stats.go           # UNCHANGED (receives already-merged inputTokens)
│   ├── token_extraction_test.go  # MODIFY: add cache token test cases
│   └── streaming_usage_test.go   # MODIFY: add cache token test cases
├── storage/
│   └── credential_usage.go  # UNCHANGED (inputTokens already includes cache)
└── transformer/
    └── types.go             # UNCHANGED (no new fields needed)
```

**Structure Decision**: 单项目结构。修改集中在 proxy 包的核心 token 提取函数，上下游代码无需变更。

## Complexity Tracking

> 无违规项需要说明。
