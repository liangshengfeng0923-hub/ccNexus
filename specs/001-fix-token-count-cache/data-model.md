# Data Model: Token 计数缓存字段

**Feature**: 修复 Token 计数遗漏缓存命中 Token
**Date**: 2026-06-18

## 概述

此修复不引入新的数据实体或修改数据库 schema。所有变更仅限于 token 提取过程的中间计算逻辑。

## 概念模型变更

### Usage（Token 用量信息）

API 响应中的 `usage` 对象——提取规则的变更：

| 字段 | 来源 | 提取规则 |
|------|------|---------|
| `input_tokens` | Anthropic Claude, Gemini | 直接取值（已包含 `cache_creation` 子集） |
| `prompt_tokens` | OpenAI Chat, OpenAI Responses | 直接取值（已包含所有 input token） |
| `cache_read_input_tokens` | Anthropic Claude, Gemini | **新增**：加到总输入 token 中 |
| `output_tokens` | Claude, Gemini | 直接取值 |
| `completion_tokens` | OpenAI Chat | 直接取值 |

### 总输入 Token 计算公式

```
总输入 Token = input_tokens (或 prompt_tokens) + cache_read_input_tokens
总 Token = 总输入 Token + output_tokens (或 completion_tokens)
```

### 数据流

```
API 响应 usage 对象
    │
    ▼
extractInputOutputTokens()
    │  提取 input_tokens/prompt_tokens
    │  提取 cache_read_input_tokens ← [新增]
    │  返回 (总输入Token, 输出Token)
    │
    ├──▶ handleNonStreamingResponse → proxy.go → RecordTokens / recordCredentialUsage
    │
    ├──▶ handleStreamingResponse → streamCtx.InputTokens → 转换器 total_tokens
    │       → proxy.go → RecordTokens / recordCredentialUsage  
    │
    └──▶ handleStreamingAsNonStreaming → proxy.go → RecordTokens / recordCredentialUsage
```

## 不受影响的实体

以下实体无需修改——它们接收的 `inputTokens` 参数在调用前已完成缓存 token 合并：

- **StatRecord** (`internal/proxy/stats.go`): `InputTokens int` 字段语义不变
- **CredentialUsage** (`internal/storage/credential_usage.go`): `input_tokens` 列语义不变
- **StreamContext** (`internal/transformer/types.go`): `InputTokens int` 字段语义不变
- **APIKeyDailyStat** (`internal/storage/`): `InputTokens` 字段语义不变
