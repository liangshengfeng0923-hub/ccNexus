# Research: Token Counting with Cache Fields

**Feature**: 修复 Token 计数遗漏缓存命中 Token
**Date**: 2026-06-18

## Decision 1: 修改 extractInputOutputTokens 而非更上层函数

**Decision**: 在 `extractInputOutputTokens` 函数中添加缓存 token 提取，将其合并到返回的 `inputTokens` 中。

**Rationale**:
- `extractInputOutputTokens` 是所有 token 提取路径的唯一汇聚点：
  - 非流式：`extractTokenUsage` → `extractInputOutputTokens`
  - 流式：`extractTokensFromEvent` → `extractInputOutputTokens`
  - 混合（Codex SSE→非流式）：`handleStreamingAsNonStreaming` → `extractTokenUsage`
- 修改这一处即可修复所有响应路径
- 上游调用者（`proxy.go` 中的 `recordCredentialUsage`、`RecordTokens`）不需要任何修改——它们接收的 `inputTokens` 已经包含缓存 token

**Alternatives considered**:
- 在每个调用点分别添加缓存 token：重复代码，容易遗漏
- 修改 `RecordTokens` 签名增加缓存参数：破坏接口，改动面大
- 新增独立字段存储缓存 token：存储层需要改 schema，当前需求不需要区分缓存类型

## Decision 2: 仅处理 Anthropic/Claude 格式的缓存字段

**Decision**: 仅对 `cache_creation_input_tokens` 和 `cache_read_input_tokens` 进行额外提取。不修改 OpenAI 格式（`prompt_tokens`/`completion_tokens`）的处理逻辑。

**Rationale**:
- Anthropic API 的 `usage` 对象：`input_tokens` = 未缓存输入 + 缓存写入的输入。`cache_read_input_tokens` 是从缓存读取的独立字段。总计费输入 = input_tokens + cache_read_input_tokens
- OpenAI API 的 `usage` 对象：`prompt_tokens` = 所有输入 token 的总和（已包含 cached tokens）。无需额外加总
- Gemini API：与 Anthropic 类似，返回独立的缓存 token 计数

**Anthropic API Usage 字段规范**（来自官方文档）:
```json
{
  "input_tokens": 100,            // 每次请求都收取费用的输入 token（含 cache_write）
  "cache_creation_input_tokens": 500,  // 写入缓存的 token 数
  "cache_read_input_tokens": 2000,     // 从缓存读取的 token 数
  "output_tokens": 300
}
```
总计费输入 token = `input_tokens` + `cache_read_input_tokens`
注：`cache_creation_input_tokens` 是 `input_tokens` 的子集（已计入），不需要重复加总。

实际上，需要再确认一下：Anthropic 的 input_tokens 是否真的包含 cache_creation_input_tokens？

根据 Anthropic 官方文档：
- `input_tokens`: The number of input tokens which were used. 这里是 TOTAL input tokens
- `cache_creation_input_tokens`: The number of input tokens used to create a new cache entry (subset of input_tokens)
- `cache_read_input_tokens`: The number of input tokens pulled from the cache

所以正确的总输入 token = `input_tokens` + `cache_read_input_tokens`

**Alternatives considered**:
- 对所有格式都搜索缓存字段：OpenAI 格式的 `prompt_tokens` 已经是总数，再重复加会重复计数

## Decision 3: 实现方式——在 extractInputOutputTokens 中增加 map lookup

**Decision**: 在现有的 `extractInputOutputTokens` 函数末尾，增加对 `cache_read_input_tokens` 的查找并将其加到返回的 `inputTokens` 中。

**Rationale**:
- 实现最简单：4 行代码
- 性能影响极小：两次额外的 map key lookup
- 向后兼容：缓存字段不存在时不会改变行为
- 符合现有代码模式

**Pseudo-code**:
```go
func extractInputOutputTokens(usage map[string]interface{}) (int, int) {
    // ... existing input_tokens/prompt_tokens extraction ...
    // ... existing output_tokens/completion_tokens extraction ...
    
    // Add cache_read tokens from Anthropic API (these are billed separately)
    if cacheRead, ok := usage["cache_read_input_tokens"]; ok {
        inputTokens += parseTokenNumber(cacheRead)
    }
    // Note: cache_creation_input_tokens is already included in input_tokens,
    // so we don't add it separately.
    
    return inputTokens, outputTokens
}
```

**Alternatives considered**:
- 同时加上 `cache_creation_input_tokens`：会导致重复计数（它是 input_tokens 的子集）
- 仅在上游是 Anthropic 格式时添加：需要格式检测，增加复杂度，不必要

## Decision 4: 不需要修改 StreamContext 或存储层

**Decision**: 不修改 `StreamContext`、`RecordTokens`、`recordCredentialUsage`、`__recordAPIKeyUsage` 或数据库 schema。

**Rationale**:
- `StreamContext.InputTokens` 已经通过 `extractTokensFromEvent` → `extractInputOutputTokens` 设置
- 修复后该字段自动包含缓存 token
- 所有下游消费者使用该字段计算 `total_tokens`、写入存储——自动正确
- Gemini 和 OpenAI 格式的 transformer 在计算 `total_tokens` 时使用 `ctx.InputTokens + ctx.OutputTokens`，修复后自动正确
