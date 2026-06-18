# Quickstart: 验证缓存 Token 计数修复

**Feature**: 修复 Token 计数遗漏缓存命中 Token
**Date**: 2026-06-18

## 前置条件

- Go 1.24+ 环境
- ccNexus 项目已构建或可运行测试
- （可选）一个真实 Anthropic API 端点用于集成验证

## 验证步骤

### 1. 运行现有测试（回归验证）

```bash
cd internal/proxy && go test -v -run "TestTokenExtraction|TestStreamingUsage" ./...
```

**预期结果**: 所有现有测试通过，缓存 token 相关的测试用例也应通过。

### 2. 单元测试：验证缓存 token 提取

新增的测试用例应覆盖：

**测试 A**：Anthropic 格式非流式响应含缓存字段
```
输入: {"usage": {"input_tokens": 100, "cache_read_input_tokens": 2000, "output_tokens": 300}}
期望: inputTokens=2100, outputTokens=300
```

**测试 B**：Anthropic 格式流式事件含缓存字段
```
输入: data: {"type":"message_start","message":{"usage":{"input_tokens":50,"cache_read_input_tokens":1000,"output_tokens":0}}}
期望: inputTokens=1050, outputTokens=0
```

**测试 C**：OpenAI 格式响应（无缓存字段，向后兼容）
```
输入: {"usage": {"prompt_tokens": 56, "completion_tokens": 78}}
期望: inputTokens=56, outputTokens=78
```

**测试 D**：缓存字段为 0
```
输入: {"usage": {"input_tokens": 100, "cache_read_input_tokens": 0, "output_tokens": 50}}
期望: inputTokens=100, outputTokens=50
```

**测试 E**：无缓存字段（向后兼容）
```
输入: {"usage": {"input_tokens": 50, "output_tokens": 80}}
期望: inputTokens=50, outputTokens=80
```

运行：`cd internal/proxy && go test -v -run "TestCacheTokens" ./...`

### 3. 集成验证（需要真实端点）

1. 在 ccNexus 中配置一个 Anthropic API 端点
2. 发送一个启用 Prompt Caching 的请求：
   ```bash
   curl -X POST http://localhost:PORT/v1/messages \
     -H "Content-Type: application/json" \
     -H "X-API-Key: YOUR_KEY" \
     -d '{
       "model": "claude-sonnet-4-6",
       "max_tokens": 100,
       "system": [{"type": "text", "text": "long system prompt...", "cache_control": {"type": "ephemeral"}}],
       "messages": [{"role": "user", "content": "Hello"}]
     }'
   ```
3. 查看统计：`curl http://localhost:PORT/stats`
4. **预期结果**: 总 Token 数包含缓存命中的输入 token

### 4. 验证统计/端点页面

如果运行桌面模式（Wails），确认：
- API Key 用量表格中"总 Token"列显示的值 >= 输入 Token + 输出 Token
- 端点统计详情中总输入 Token 包含缓存部分

## 退出标准

- [ ] `go test ./internal/proxy/...` 全部通过
- [ ] `go vet ./internal/proxy/...` 无新增警告
- [ ] 新增缓存 token 测试用例全部通过
- [ ] 现有测试无一失败
