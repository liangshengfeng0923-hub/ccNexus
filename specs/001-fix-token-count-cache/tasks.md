# Tasks: 修复 Token 计数遗漏缓存命中 Token

**Input**: Design documents from `specs/001-fix-token-count-cache/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, quickstart.md

**Tests**: 本次修复包含测试任务——spec 中定义了验收场景，需通过新增测试用例验证。

**Organization**: 三个用户故事由同一处代码修复解决（`extractInputOutputTokens`），测试任务按故事拆分以验证各自路径。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行执行（不同文件，无依赖）
- **[Story]**: 关联的用户故事（US1, US2, US3）
- 描述中包含精确文件路径

---

## Phase 1: Core Implementation（核心修复）

**Purpose**: 修改 token 提取核心逻辑，一次性修复流式/非流式/混合三条路径。

**⚠️ 注意**: 此阶段修改一处函数即可同时满足 US1/US2/US3 的所有需求。

- [x] T001 在 `internal/proxy/response.go` 的 `extractInputOutputTokens` 函数中增加 `cache_read_input_tokens` 字段的提取，将其值加到返回的 `inputTokens` 中

**Checkpoint**: 核心修复完成——所有三条路径的 token 计数已包含缓存读取 token。可进入测试阶段验证。

---

## Phase 2: User Story 1 - 统计准确性验证 (Priority: P1) 🎯 MVP

**Goal**: 验证非流式 JSON 响应的 token 提取正确包含缓存读取 token

**Independent Test**: `go test -v -run "TestCacheTokens|TestExtractTokenUsage" ./internal/proxy/`

### Tests for User Story 1

- [x] T002 [P] [US1] 在 `internal/proxy/token_extraction_test.go` 中新增测试用例：Anthropic 非流式响应含 `cache_read_input_tokens` 字段时，验证总输入 token = input_tokens + cache_read_input_tokens
- [x] T003 [P] [US1] 在 `internal/proxy/token_extraction_test.go` 中新增测试用例：API 响应无缓存字段时向后兼容（行为不变）
- [x] T004 [P] [US1] 在 `internal/proxy/token_extraction_test.go` 中新增测试用例：`cache_read_input_tokens=0` 时不影响正常计数
- [x] T005 [P] [US1] 在 `internal/proxy/token_extraction_test.go` 中新增测试用例：OpenAI 格式 `prompt_tokens`/`completion_tokens` 不重复加总（仅对 Anthropic 缓存字段生效）

**Checkpoint**: 非流式路径的缓存 token 提取已验证通过

---

## Phase 3: User Story 2 - 流式响应验证 (Priority: P1)

**Goal**: 验证流式 SSE 响应的 token 提取（通过 `extractTokensFromEvent` → `extractInputOutputTokens`）正确包含缓存读取 token

**Independent Test**: `go test -v -run "TestStreamingUsage|TestCacheTokens" ./internal/proxy/`

### Tests for User Story 2

- [x] T006 [P] [US2] 在 `internal/proxy/streaming_usage_test.go` 中新增测试用例：`message_start` 事件的 `message.usage` 包含 `cache_read_input_tokens` 时，验证流式提取正确加总
- [x] T007 [P] [US2] 在 `internal/proxy/streaming_usage_test.go` 中新增测试用例：`message_delta` 事件的 `usage` 包含 `output_tokens`，验证输入+输出合计正确

**Checkpoint**: 流式路径的缓存 token 提取已验证通过

---

## Phase 4: User Story 3 - 混合模式验证 (Priority: P2)

**Goal**: 验证 Codex SSE→非流式聚合路径中缓存 token 正确处理

**Independent Test**: `go test -v -run "TestStreamingAsNonStreaming|TestCacheTokens" ./internal/proxy/`

### Tests for User Story 3

- [x] T008 [P] [US3] 在 `internal/proxy/streaming_usage_test.go` 中新增测试用例：Codex 流式聚合为非流式响应时，`extractTokenUsage` 正确提取缓存 token
- [x] T009 [US3] 在 `internal/proxy/token_extraction_test.go` 中新增测试用例：Gemini 格式的 usage 结构含缓存字段时，验证提取逻辑

**Checkpoint**: 所有三条路径的缓存 token 提取已验证通过

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 最终验证与代码质量检查

- [x] T010 运行 `go test ./internal/proxy/... -count=1` 确认所有新增和现有测试通过
- [x] T011 运行 `go vet ./internal/proxy/...` 检查无新增警告
- [x] T012 执行 quickstart.md 中的集成验证场景（需要真实 Anthropic API 端点）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Core Implementation)**: 无依赖——直接修改代码
- **Phases 2-4 (Tests)**: 全部依赖 Phase 1 完成（需要代码修改后才能写/跑测试）
  - 但 Phase 2/3/4 之间相互独立，可并行执行
- **Phase 5 (Polish)**: 依赖 Phases 2-4 全部完成

### User Story Dependencies

- **User Story 1 (P1)**: 可独立验证——仅需非流式响应测试
- **User Story 2 (P1)**: 可独立验证——仅需流式响应测试
- **User Story 3 (P2)**: 可独立验证——仅需混合模式测试
- 三个故事共享同一处代码修改，但验证相互独立

### Within Each Phase

- T001 必须在所有测试任务之前完成
- Phase 2/3/4 内的所有 [P] 任务可并行执行
- T009 依赖 T001（需要确认 Gemini 格式兼容性）

### Parallel Opportunities

- Phase 2 内 T002-T005 全部 [P]：4 个测试用例可同时编写（不同测试函数）
- Phase 3 内 T006-T007 全部 [P]：2 个测试用例可同时编写
- Phase 2/3/4 整体可并行启动（如果有多个开发者）

---

## Parallel Example: Phase 2 Tests

```bash
# 四个非流式测试用例可同时编写（不同测试函数，无冲突）：
Task: "T002 [US1] Anthropic 非流式响应含 cache_read_input_tokens 测试"
Task: "T003 [US1] 向后兼容测试（无缓存字段）"
Task: "T004 [US1] cache_read_input_tokens=0 测试"
Task: "T005 [US1] OpenAI 格式不重复加总测试"
```

---

## Implementation Strategy

### MVP First (仅 Phase 1 + Phase 2)

1. 完成 T001：修改 `extractInputOutputTokens`（~4 行代码）
2. 完成 Phase 2：非流式测试（4 个测试用例）
3. **STOP and VALIDATE**: `go test -v -run "TestCacheTokens|TestExtractTokenUsage" ./internal/proxy/`
4. 此时核心修复已完成并可验证——MVP 就绪

### Incremental Delivery

1. T001 → 核心修复完成
2. Phase 2 (US1) → 非流式验证通过
3. Phase 3 (US2) → 流式验证通过
4. Phase 4 (US3) → 混合模式验证通过
5. Phase 5 → 最终回归 + 代码质量检查
6. 每一阶段后均可独立验证

### 单人开发策略（推荐）

此修复改动量极小，建议按顺序执行：
1. T001（修改代码）→ 5 分钟
2. Phase 2-4 所有测试任务（可合并为一次提交）→ 15 分钟
3. T010-T012（验证）→ 5 分钟
4. **总预计工时：约 25 分钟**

---

## Notes

- 核心修改仅涉及 `extractInputOutputTokens` 函数（`internal/proxy/response.go:88`），约 4 行新增代码
- 不需要修改数据库 schema、存储层、转换器或对外 API
- `cache_creation_input_tokens` 是 `input_tokens` 的子集，不需要额外加总
- `cache_read_input_tokens` 是独立计费的缓存读取 token，需要加到总输入中
- OpenAI 格式的 `prompt_tokens` 已包含所有输入 token，不适用缓存字段补丁
- 向后兼容：上游 API 不返回缓存字段时行为完全不变
