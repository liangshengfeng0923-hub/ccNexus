# Feature Specification: 修复 Token 计数遗漏缓存命中 Token 的问题

**Feature Branch**: `001-fix-token-count-cache`

**Created**: 2026-06-18

**Status**: Draft

**Input**: User description: "总令牌数计算好像存在问题，目前估算是等于 缓存未命中的输入+输出，没有计算缓存命中的输入token"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 统计页面准确反映实际 Token 消耗 (Priority: P1)

作为 ccNexus 管理员，当我查看 API Key 用量统计或端点统计数据时，我希望"总 Token 数"能够准确反映所有实际消耗的 Token，包括通过 Prompt Caching 机制从缓存中读取的输入 Token，而不仅仅是未命中的输入 Token 加上输出 Token。

**Why this priority**: 这是核心问题——当前统计的总 Token 数严重低估了实际消耗，导致管理员无法准确评估 API 使用成本和配额消耗。Anthropic API 对缓存命中的 Token 也会计费（按照缓存读取价格），因此这些 Token 必须被计入总用量。

**Independent Test**: 发送一个启用 Prompt Caching 的请求，检查统计页面显示的总 Token 数是否等于 `input_tokens + cache_creation_input_tokens + cache_read_input_tokens + output_tokens`。

**Acceptance Scenarios**:

1. **Given** 上游 API 返回的 usage 包含 `input_tokens=100, cache_creation_input_tokens=500, cache_read_input_tokens=2000, output_tokens=300`，**When** 请求完成，**Then** 系统记录的总输入 Token 为 `100 + 500 + 2000 = 2600`，总 Token 为 `2600 + 300 = 2900`
2. **Given** 上游 API 返回的 usage 仅包含 `input_tokens=50, output_tokens=80`（无缓存字段），**When** 请求完成，**Then** 系统记录的总输入 Token 为 `50`，总 Token 为 `50 + 80 = 130`（向后兼容）

---

### User Story 2 - 流式响应中正确提取缓存 Token (Priority: P1)

作为 ccNexus 代理，当处理 SSE 流式响应时，我需要从 `message_start` 等事件中提取 `cache_creation_input_tokens` 和 `cache_read_input_tokens`，并将它们计入最终统计。

**Why this priority**: 流式响应是主要使用场景，`message_start` 事件中的 `usage` 对象包含完整的输入 token 信息（包括缓存相关字段），当前代码未提取这些额外字段。

**Independent Test**: 发送一个启用流式传输的请求，验证从 SSE 事件流中提取的 token 计数包含缓存命中 token。

**Acceptance Scenarios**:

1. **Given** SSE 流中包含 `message_start` 事件，其 `message.usage` 包含 `input_tokens=50, cache_creation_input_tokens=300, cache_read_input_tokens=1000`，**When** 流式响应处理完成，**Then** 提取的总输入 Token 为 `50 + 300 + 1000 = 1350`
2. **Given** SSE 流中的 `message_delta` 事件，其 `usage` 包含 `output_tokens=200`，**When** 流式响应处理完成，**Then** 提取的输出 Token 为 `200`，且与输入 Token 合计为 `1550`

---

### User Story 3 - 非流式响应中正确提取缓存 Token (Priority: P2)

作为 ccNexus 代理，当处理非流式 JSON 响应时，我需要从 `usage` 对象中提取 `cache_creation_input_tokens` 和 `cache_read_input_tokens` 字段。

**Why this priority**: 非流式响应也需要修复，但使用频率通常低于流式响应。逻辑上需要在同一函数中修改，实现成本相近。

**Independent Test**: 发送一个非流式请求，验证统计中包含了缓存相关 Token。

**Acceptance Scenarios**:

1. **Given** 非流式响应 JSON 的 `usage` 对象包含 `input_tokens=200, cache_read_input_tokens=800, output_tokens=150`，**When** 响应处理完成，**Then** 提取的总输入 Token 为 `200 + 800 = 1000`

---

### Edge Cases

- 上游 API 不返回 `cache_creation_input_tokens` 和 `cache_read_input_tokens` 字段时，系统应优雅降级，仅使用 `input_tokens` 的值（向后兼容）
- 缓存字段值为 `0` 或 `null` 时，不应影响正常的 token 计数
- OpenAI 格式的 `prompt_tokens` 字段（已包含缓存 token）应保持现有行为，不应重复加总
- Gemini 格式的 usage 结构如有差异，需确认兼容性
- 当上游 API 同时提供 `total_tokens` 和各个分量时，应以各分量之和（`input_tokens + cache_creation_input_tokens + cache_read_input_tokens + output_tokens`）作为统计依据，确保跨不同 API 格式统计口径一致

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统必须从 API 响应的 `usage` 对象中提取 `cache_creation_input_tokens` 字段
- **FR-002**: 系统必须从 API 响应的 `usage` 对象中提取 `cache_read_input_tokens` 字段
- **FR-003**: 系统的总输入 Token 计数必须等于 `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` 三者之和
- **FR-004**: 系统的总 Token 计数必须等于总输入 Token + `output_tokens`
- **FR-005**: 当上游 API 响应不包含 `cache_creation_input_tokens` 或 `cache_read_input_tokens` 字段时，系统必须将这些字段视为 `0`，不影响现有计数逻辑
- **FR-006**: 流式响应的 token 提取必须支持缓存相关字段（`message_start` 和 `message_delta` 事件中的 `usage`）
- **FR-007**: 非流式响应的 token 提取必须支持缓存相关字段
- **FR-008**: 新增的缓存 Token 必须纳入端点凭证维度的用量统计记录中
- **FR-009**: 新增的缓存 Token 必须纳入 API Key 维度的用量统计记录中
- **FR-010**: API 格式转换过程中（Claude ↔ OpenAI ↔ Gemini），缓存 token 信息应尽可能保留和转发

### Key Entities

- **Usage（Token 用量）**: 单次 API 请求的 token 消耗记录。关键属性：`input_tokens`（非缓存输入）、`cache_creation_input_tokens`（新写入缓存的输入 token）、`cache_read_input_tokens`（从缓存读取的输入 token）、`output_tokens`（输出 token）。其中总输入 token = 前三者之和。
- **CredentialUsage（凭证用量统计）**: 按凭证维度聚合的用量统计记录，需将缓存创建和缓存读取的 token 纳入总输入 token 的计数中。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 启用 Prompt Caching 的请求，统计页面显示的"总 Token 数"与上游 API 返回的实际计费 token 数一致（误差为 0）
- **SC-002**: 缓存 Token 计数功能不破坏现有统计——不启用缓存的请求，统计结果与修复前完全一致
- **SC-003**: 流式和非流式两种响应路径的缓存 token 计数行为一致
- **SC-004**: 所有现有测试继续通过，新增的缓存 token 提取行为被测试覆盖

## Assumptions

- Anthropic API 的 `usage` 对象中 `input_tokens` 仅表示非缓存的输入 token 数量，不包括缓存命中和缓存创建的 token
- `cache_creation_input_tokens` 和 `cache_read_input_tokens` 的 JSON 字段名与 Anthropic 官方 API 规范一致
- 对于 OpenAI 格式的响应（使用 `prompt_tokens`/`completion_tokens`），其 `prompt_tokens` 已经包含了所有输入 token（包括潜在的缓存相关 token），无需额外处理
- 统计存储层将总输入 token（包含缓存 token）合并计入现有输入 token 字段，无需新增独立的存储列
- 此修复仅涉及 token 计数逻辑，不影响请求/响应的实际转换逻辑
