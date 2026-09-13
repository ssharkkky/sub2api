# FORK_MODEL_REFACTOR_PLAN.md 审计报告

## 总体结论
**该文档方向正确、核心设计成熟，但暂不可直接实施，需修订后执行。**
**置信度：高。**

文档在架构原则（零默认映射、透传为默认态、纯显式映射、删除分组 allowlist）上十分清晰，业务推导严密。然而，经对照当前 worktree（`v0.2.2-ts.1`）代码，发现文档存在**关键调度路径遗漏（未覆盖核心网关 `gateway_scheduling.go` 中的 `mapAntigravityModel` 与调度链路）**、**部分行号微漂移**、**PR 影响面与测试范围遗漏（未纳入迁移 275 的 schema/mutation 影响以及多个测试文件）**，以及**发布/迁移操作时序有待澄清**。

---

## 发现清单

### [阻断] F-01: 核心调度路径遗漏 `mapAntigravityModel` 及 `gateway_scheduling.go` 的重构
- **严重度**：阻断 (Blocker)
- **问题描述**：
  文档第 4.1 节与 PR-A 集中在 `account.go:IsModelSupported` 和 `openai_gateway_scheduling.go:483`。但实际上，整个系统的核心网关调度入口是 `backend/internal/service/gateway_scheduling.go:2774`（`isModelSupportedByAccountWithContext`）和 `:2831`（`isModelSupportedByAccount`）。更关键的是，针对 Antigravity 平台，代码硬编码调用了 `mapAntigravityModel(account, requestedModel)`（`backend/internal/service/antigravity_gateway_service.go:264`）。
  `mapAntigravityModel` 内部调用了 `account.GetModelMapping()`，当空映射被置为 `nil` 后，如果未同步快照，`mapAntigravityModel` 将直接返回空字符串 `""`，导致 `isModelSupportedByAccount` 返回 `false`，不仅请求无法调度，连透传逻辑都会直接被截断！
  此外，`:2858` 行的 `isModelSupportedByAccount` 对非 Antigravity 账号执行：
  ```go
  if !account.IsModelSupported(requestedModel) {
      return false
  }
  return account.HasSyncedUpstreamModel(requestedModel)
  ```
  如果新版 `IsModelSupported` 内部已经判定了 `HasSyncedUpstreamModel`，此处再无条件调一次 `HasSyncedUpstreamModel`，逻辑虽尚可，但两层检查存在冗余与语义耦合。
- **证据**：
  - `backend/internal/service/gateway_scheduling.go:2774-2863`
  - `backend/internal/service/antigravity_gateway_service.go:264-288`
  - `backend/internal/service/gateway_forward.go:1027`
- **建议修法**：
  在计划文档 §4.1、§7 (PR-A) 中增加对 `antigravity_gateway_service.go`（`mapAntigravityModel`）和 `gateway_scheduling.go`（`isModelSupportedByAccountWithContext` / `isModelSupportedByAccount`）的重构方案，明确零映射下 Antigravity 账号如何透传或改写，避免路由被 `mapAntigravityModel` 提前阻断。

---

### [重要] F-02: 落地顺序与发布时序存在不一致（§8 与 §4.2 步骤 4 矛盾）
- **严重度**：重要 (Major)
- **问题描述**：
  在 §4.2 迁移步骤中写明：“1. 只读拉清单... 2. 显式写入 model_mapping... 3. 逐分组验证... 4. **全部补齐后再发布代码**”。
  但在 §8 落地顺序中却写为：
  “1. 生产只读排查... 2. **PR-A（零默认映射 + 路由回退）→ 按 SOP 审计合并**... 3. 生产数据迁移... 4. PR-B/C/D... 5. 发布”。
  如果第二步就把 PR-A 合并甚至误发布，存量依赖默认映射的账号将瞬间路由失败；此外，若未做数据迁移，PR-A 的零默认映射代码不能提前发布到生产。
- **证据**：
  - 文档 §4.2（行 106-118） vs §8（行 149-158）
- **建议修法**：
  明确区分“Git 分支合并顺序”与“生产环境部署发布顺序”。规范化 §8 为：
  1. 生产只读排查（拉清单）；
  2. 生产数据预迁移（在当前代码版本下，提前给账号补齐显式 `model_mapping`；当前版本本就支持显式映射覆盖默认映射，因此预补齐显式映射对当前生产零破坏）；
  3. 生产线上验证（确认显式映射已生效）；
  4. 代码研发与 PR 推进（PR-A ~ PR-D 并行或串行审查合并）；
  5. 蓝绿发布新二进制。

---

### [重要] F-03: R3 删除分组 `model_allowlist` 遗漏 Ent 衍生代码及关联模块
- **严重度**：重要 (Major)
- **问题描述**：
  文档 §7 PR-C 罗列了 allowlist 的删除清单，但遗漏了若干重要位置：
  1. `backend/internal/ent/` 目录下大量由 schema 生成的 mutation / builder / predicate 代码（虽然列保留，但如果不更新 ent schema 或重新生成，编译和 lint 会报错）；
  2. `backend/internal/handler/admin/group_handler.go:243, 328`（DTO 绑定）；
  3. `backend/internal/handler/dto/types.go:204`；
  4. `backend/internal/service/api_key_auth_cache.go:111` 及 `api_key_auth_cache_impl.go:17`（API Key 鉴权缓存结构体中仍缓存了 `ModelAllowlist`，删掉后必须注意缓存版本号自增或清空策略，防缓存反序列化 panic）；
  5. 集成测试 `backend/internal/repository/api_key_repo_model_allowlist_projection_integration_test.go`。
- **证据**：
  - `backend/internal/handler/admin/group_handler.go:243`
  - `backend/internal/service/api_key_auth_cache.go:111`
  - `backend/internal/repository/api_key_repo_model_allowlist_projection_integration_test.go:48`
- **建议修法**：
  在 §7 PR-C 补全上述 handler、dto、cache 及测试文件清单，并明确 `api_key_auth_cache` 缓存版本更新事项。

---

### [重要] F-04: Anthropic OAuth/SetupToken 短名到长名映射的归属需明确
- **严重度**：重要 (Major)
- **问题描述**：
  在 `gateway_scheduling.go:2820-2826` 和 `gateway_forward.go:294-302` 中，Anthropic 账号如果是非 APIKey 类型（如 OAuth / ServiceAccount），系统会自动通过 `claude.NormalizeModelID(requestedModel)` 和 `normalizeVertexAnthropicModelID` 把短模型名改写为带有日期的长模型名或 Vertex 格式。
  文档在 §3 中区分了“默认映射（删）”与“账号别名（留，如 Bedrock）”，并保留了 `isOpenAIOAuthServableModel`，但**完全未提及 Claude OAuth / Vertex 的这一层长短名转换机制**。这属于“账号别名机制（留）”还是需要由运维显式配置？如果不保留且未配置，会导致 Claude OAuth 账号上游调用报错。
- **证据**：
  - `backend/internal/service/gateway_scheduling.go:2822-2824`
  - `backend/internal/service/gateway_forward.go:294-302`
  - `backend/internal/pkg/claude/constants.go:230-240`
- **建议修法**：
  在 §3 的“要留的账号别名”表格中，补入 `claude.NormalizeModelID` 与 `normalizeVertexAnthropicModelID`，明确将其定性为“底层上游协议别名机制（保留）”，消除实现歧义。

---

### [次要] F-05: 行号引用微小漂移
- **严重度**：次要 (Minor)
- **问题描述**：
  文档中的部分行号与实际 worktree 代码存在少许行号偏差（约 5~15 行）：
  - `account.go` 中 `ensureAntigravityDefaultPassthroughs` 实际位于 line 754（文档注 :756），`applyAntigravityGemini31ProAliases` 实际位于 line 760（文档注 :762），调用点实际位于 line 673-683（文档注 :674-684）。
  - `openai_gateway_scheduling.go` 中 `isAccountModelSupportedForRequest` 实际位于 line 480（文档注 :483）。
  - `account.go` 中 `ModelMappingRestricts` 实际位于 line 881（文档注 :884）。
- **证据**：
  - `backend/internal/service/account.go:673, 754, 760, 881`
  - `backend/internal/service/openai_gateway_scheduling.go:480`
- **建议修法**：
  在计划文档中对齐实际行号，或注明以函数名为准。

---

### [建议] F-06: 建议明确通配符 key 展开在 R4 中的具体算法细节
- **严重度**：建议 (Suggestion)
- **问题描述**：
  文档 §4.3 指出：“通配符 key 展开源：账号原生快照；快照未命中再回退平台 catalog 并标警告”。
  在实现 `storefrontModelUnion` 时，如果账号配置了 `gpt-*` 或 `*`，展开快照可能产生数十上百个模型。当账号尚未同步（快照为空）且有通配符时，回退平台 catalog 展开的具体规则（是匹配 catalog 所有满足 pattern 的模型还是如何）需有明确伪代码，避免 PR-B 实现时反复推敲。
- **证据**：
  - 文档 §4.3
  - `backend/internal/service/channel_storefront.go:176`
- **建议修法**：
  在 PR-B 说明中补充一小段展开算法伪代码（Pattern Match 遍历快照 / 回退 catalog）。

---

## 代码交叉核对表

| 检查项 | 文档表述与引用 | 实际代码位置与状态 | 结论 | 备注 |
|---|---|---|---|---|
| **第 1 层默认映射** | `account.go:639 resolveStoredModelMapping`<br>平台默认：Antigravity (:643,663)、Kiro (:646,663)、Grok (:649,666,705)、Gemini (:656,695) | `backend/internal/service/account.go:639-715` | **一致** | 行号及分支逻辑完全匹配（含 nil credentials 和 empty mapping 两处检查） |
| **第 2 层默认映射** | `platform_default_models.go:90 applyCatalogDefaultMappings` 调用 `modelcatalog.DefaultMappings` | `backend/internal/service/platform_default_models.go:90` | **一致** | 逻辑与调用行号完全匹配 |
| **第 3 层默认映射** | `account.go:756,762 ensureAntigravityDefaultPassthroughs` + `applyAntigravityGemini31ProAliases`（调用点 :674-684） | `backend/internal/service/account.go:673-683, 754, 760` | **基本一致** | 存在 1~2 行微小偏差，函数名与逻辑一致 |
| **第 4 层默认映射** | `pkg/xai/models.go:27,150 RuntimeModelMappingVersion/DefaultModelMapping`<br>`openai_messages_dispatch.go:75`<br>`setting_parse.go:1006` | `backend/internal/pkg/xai/models.go:27, 150`<br>`backend/internal/service/openai_messages_dispatch.go:73`<br>`backend/internal/service/setting_parse.go:1006` | **一致** | 完全匹配 |
| **第 5 层默认映射** | `geminicli.GoogleOneModelMapping()`（引用点 `account.go:656,695`） | `backend/internal/pkg/geminicli/models.go:36`<br>`backend/internal/service/account.go:656, 695` | **一致** | 完全匹配 |
| **Bedrock 别名保留** | `bedrock_request.go:129 normalizeBedrockModelID` 查 `DefaultBedrockModelMapping`<br>`bedrock_request.go:23-61 AdjustBedrockModelRegionPrefix` | `backend/internal/service/bedrock_request.go:129`<br>`backend/internal/service/bedrock_request.go:23-61` | **一致** | 完全匹配，确为别名改写 |
| **OpenAI 守卫保留** | `account.go isOpenAIOAuthServableModel` | `backend/internal/service/account.go:911, 918, 936` | **一致** | 确用于防 Codex 400 |
| **IsModelSupported** | `account.go:905 IsModelSupported` 现状无映射返回 true | `backend/internal/service/account.go:905-926` | **一致** | 确实现为 `len(mapping)==0` 时返回 `true` |
| **R4 storefront 函数** | `channel_storefront.go`<br>`storefrontModelUnion:176`<br>`storefrontModelsFromUnion:196`<br>`ListCatalogStorefrontModelsWithCoverage:117`<br>`AnnotateCatalogStorefrontCoverage:267`<br>`filterStorefrontCoverageAccounts:229` | `backend/internal/service/channel_storefront.go`<br>`:176` (storefrontModelUnion)<br>`:196` (storefrontModelsFromUnion)<br>`:117` (ListCatalogStorefrontModelsWithCoverage)<br>`:267` (AnnotateCatalogStorefrontCoverage)<br>`:229` (filterStorefrontCoverageAccounts) | **完全一致** | 所有行号与函数名 100% 精确吻合 |
| **R3 allowlist 范围** | `handler/openai_gateway_handler.go:3732` 及前端/后端多处 | `backend/internal/handler/openai_gateway_handler.go:3732` | **一致** | 但遗漏部分 ent schema 衍生与 DTO 文件（见 F-03） |
| **前端相关组件** | `ChannelsView.vue:504-555` catalogPicker | `frontend/src/views/admin/ChannelsView.vue:504-555` | **完全一致** | 模板与逻辑位置精准吻合 |

---

## 需用户拍板项

在进入实施前，建议对以下 2 项进行明确确认：

1. **生产数据补齐的时序与方案（确认 F-02）**：
   - 确认是否同意在部署代码前，**先在线上执行数据脚本**，给依赖默认映射的账号写入显式映射。由于当前版本的代码也是“显式映射优先于默认映射”，提前在线上写入显式映射**对现有生产环境完全透明且安全**，能够实现真正的零下线、零 404 割接。
2. **Claude OAuth / Vertex 模型标准化的处理（确认 F-04）**：
   - 确认是否将 `claude.NormalizeModelID` 与 `normalizeVertexAnthropicModelID` 正式列入“保留的底层协议别名机制”（与 Bedrock 机制并列），不对其进行零默认删除。
