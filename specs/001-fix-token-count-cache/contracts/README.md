# Interface Contracts

**Feature**: 修复 Token 计数遗漏缓存命中 Token
**Date**: 2026-06-18

## 对外接口

无变更。所有对外 HTTP API (`/stats`, `/health`, `/v1/models`, `/`) 保持原有格式和语义。

## 内部接口

无变更。`extractInputOutputTokens` 的签名保持不变：

```go
func extractInputOutputTokens(usage map[string]interface{}) (int, int)
```

返回值语义微调：之前 `inputTokens` 仅包含 `input_tokens`（或 `prompt_tokens`），现在包含缓存读取 token。所有调用者无需修改。

## 统计接口语义

`/stats` 端点返回的 `input_tokens` 字段的语义变化：
- **修复前**：仅非缓存的输入 token（不含 cache_read）
- **修复后**：所有输入 token 的总和（含 cache_read）

这是一个向后兼容的修正——统计值更准确，格式不变。
