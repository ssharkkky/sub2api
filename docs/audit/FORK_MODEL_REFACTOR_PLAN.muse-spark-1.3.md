# PR #174 独立审计报告（Muse Spark 1.3，第 4 轮：实现审计）

- 审计对象：`codex/model-refactor` → `main`（base `36e19f93c`，HEAD `c5af50042`，12 commits，72 files，+1381/−3743）
- PR：https://github.com/ssharkkky/sub2api/pull/174
- 审计方式：只读（未修改任何文件）。前 3 轮（Gemini/Opencode/Codex）审的是**计划文档**；本轮审的是**实现代码 + 测试证据**。
- 审计日期：2026-09-13；worktree `/root/paseo/sub2api-sync-021`

## 1. 结论：FAIL（阻塞合并）

实现方向正确，5 条核心不变式在**主路径**上成立；但分支**破坏了 CI 门禁 `make test-unit`**（`backend-ci.yml:174`），且混入一个不应提交的文件。修复后可合并。

- Blocker-1：`go test -tags unit ./internal/service/` **41 个测试失败**（base 同集合全过，属分支引入）。PR-A 删除默认映射语义，但 `//go:build unit` 测试套件未同步更新——正是第 1 轮审计 M5/F-08 预警的“测试面低估”在实现阶段兑现。
- Blocker-2：`go vet/test -tags unit ./internal/handler/` **构建失败**（`gemini_v1beta_handler_test.go:20,52` 引用 PR-C 已删除的 `filterUpstreamGeminiModelsBody`）。
- Blocker-3（卫生）：`84ff29e1e` 提交了 `frontend/node_modules` **symlink 文件**（指向 `/root/paseo/sub2api/frontend/node_modules` 绝对路径），新鲜 clone 必坏，必须移除。
- 此前各 commit 自称的“59 包 ok / 0 FAIL”只覆盖了**无 tag 套件**（`go test ./...`），从未跑过 CI 真正 gate 的 `make test-unit`。证据链口径需修正。

## 2. 五条核心不变式核验

### ① 渠道定价唯一约束 ✅（主路径成立）
- 请求门禁 middleware 已删：`internal/server/middleware/group_model_allowlist.go` + 测试删除；`routes/gateway.go` 6 处挂载删除（`bf24bce92`）。
- `GroupModelAllowlist.Allows` 恒 `true`（`service/group_model_allowlist.go:92`），`FilterForListing` 直接返回 source（`:100`）。
- handler 层 allowlist 过滤/拒绝删除：`gateway_handler.go`、`/v1/models` composite/regular、Codex models、`batch_image_handler.go`、`gemini_v1beta_handler.go`、OpenAI WS 首帧/多轮（`bf24bce92`）。
- 残留（见 §3）：`openai_models_list.go:197` 的 allowlist-append、`supplementUnmappedOpenAIModels` 的平台默认补齐、`ModelAllowlistEnabled()` 过时注释。

### ② 零默认映射 ✅（映射层成立；有一处顺序偏离，见 Blocker 候选 Major-1）
- `resolveStoredModelMapping` 只返回显式存储，空 → nil（`service/account.go:641`）；`applyCatalogDefaultMappings` 删除（`platform_default_models.go`，`modelcatalog` import 已清）；Antigravity passthrough/alias 增补函数删除；`ModelMappingRestricts()` 删除。
- 无生产代码引用已删符号：`applyCatalogDefaultMappings`/`ensureAntigravity*`/`GetAntigravityDefaultModelMapping`/`GetKiroDefaultModelMapping`/preset fetch 均为 0 引用。
- `xai.DefaultModelMapping()`（`pkg/xai/models.go:150`）与 `geminicli.GoogleOneModelMapping()` 已无生产调用方但**未按计划删除**（Minor-2；其单测仍在，通过）。
- `IsModelSupported` 4 分支落地（`account.go:764`）：OpenAI passthrough 短路 → OAuth 无显式映射守卫 → 显式 keys 命中（含归一化回退）→ `HasSyncedUpstreamModel` 透传回退。与 §4.1 修订后伪代码一致。

### ③ 账号可服务 = 显式 keys ∪ 原生快照 ✅（路由判定成立）
- `IsModelSupported` 语义即 keys ∪ 快照；`gateway_scheduling.go:2855` 不再叠加二次快照检查（注释说明了原因：显式命中不应被要求进快照）。
- PR-B `storefrontModelUnion` = 快照 ∪ 显式 keys（exact 直接加入；wildcard 经 `expandWildcardForStorefront` 按 P2 先快照后 `PlatformDefaultModelIDs`，无匹配不编造：`channel_storefront.go:226`）。
- coverage 改证据型 `storefrontAccountCoversModel`（`:364`：mapping 命中或快照命中；无快照无映射不计入）。注释中“fail-open”措辞与实现矛盾（Nit-3）。

### ④ 两层映射 A→B→C ⚠️（基本成立，有一处实现偏离，Major-1）
- 渠道 `model_mapping`、定价、`billing_model_source`（requested/upstream/channel_mapped/response，`channel_service.go:123,353`）保留；Grok 分组 dispatch（`openai_messages_dispatch.go:75` + 设置项）保留（P3）。
- **偏离**：`mapAntigravityModel`（`antigravity_gateway_service.go:264`）**快照优先**：请求名命中快照即直接透传返回，即使存在显式非恒等改写。这与 §5 “P 命中 mapping → 改写成 U”矛盾——快照与映射重叠的名字上，显式改写被静默忽略。转发（`gateway_forward.go:1027`）、调度（`gateway_scheduling.go:2792,2836`）、限流 key（`model_rate_limit.go:142`）共 5 处调用方全部受影响。该重叠场景**无测试覆盖**（现存 `NewSnapshotModelIgnoresLegacyMapping` 只覆盖“映射缺新模型”方向）。典型受害：运维写 `claude-opus-4-6 → claude-opus-4-6-thinking` 而快照原生含 `claude-opus-4-6` 时，改写永不生效。建议改为显式映射优先（命中改写即返回；否则快照透传；否则透传）+ 补重叠场景单测。

### ⑤ Bedrock 别名保留 ✅
- `DefaultBedrockModelMapping` 保留（`domain/constants.go:103`），`normalizeBedrockModelID`（`bedrock_request.go:124`）+ `AdjustBedrockModelRegionPrefix`（`:53`）保留；Bedrock 测试仅保留存（`domain/constants_test.go:7`）。
- Claude 长短名/Vertex 归一化保留（`gateway_scheduling.go` Anthropic 分支、P8）。

## 3. 发现的问题

### Blocker
- **B-1（测试/CI）**：`go test -tags unit ./internal/service/` 41 失败，全部的分fail在 base 同命令下通过。失败簇：`account_wildcard_test.go`（`TestAccountIsModelSupported` 4 子项、`TestAccountGetModelMapping_Antigravity*` 4 项、`TestGrokAccountModelMappingCacheInvalidatesWithRuntimeSettings`）、`antigravity_model_mapping_test.go:205`（全部“默认映射”用例）、`gateway_service_antigravity_whitelist_test.go`（NoMapping/CustomMapping/ThinkingMode/SelectAccount*/StickyModelMismatch 等）、`gateway_channel_restriction_test.go`（`TestIsUpstreamModelRestrictedByChannel_UnsupportedModel` 等）、`TestMapAntigravityModel_WildcardTargetEqualsRequest`、`TestIsModelSupported_OpenAIOAuthExplicitMappingUnchanged`、`TestImagePlaygroundModelEligible_ChannelMappedRequiresExecutableAccountMapping`、`TestHandleUpstreamError_429_NonModelRateLimit_UsesMappedModelKey`、`TestPromptAuditFallbackAccountSupportsModel`、`TestDiagnoseModelAvailabilityForPlatform_NoMatchingModel_ReturnsNotFoundSignal`、Grok 转发簇（`TestForwardGrok*` ~10 项）、`TestGeminiMessagesCompatService_*` 2 项。修法：按零默认新语义重写断言（显式 mapping/snapshot fixture），而非删除文件。
- **B-2（测试/构建）**：`internal/handler` 在 `-tags unit` 下编译失败：`gemini_v1beta_handler_test.go:20,52: undefined: filterUpstreamGeminiModelsBody`（PR-C `bf24bce92` 改生产文件未动其 unit 测试；base 下 `go vet -tabs unit` 通过）。修法：同步更新/删除该测试用例。
- **B-3（卫生）**：`frontend/node_modules`（symlink → `/root/paseo/sub2api/frontend/node_modules`）被 `84ff29e1e` 提交。修法：`git rm --cached frontend/node_modules` 并加 `.gitignore` 兜底（`frontend/.gitignore` 若缺失）。

### Major
- **M-1（语义偏离）**：`mapAntigravityModel` 快照优先导致显式改写在重叠名上静默失效（§2-④）。建议显式优先 + 补测试。
- **M-2（展示残留的默认注入）**：`supplementUnmappedOpenAIModels`（`group_model_allowlist.go:33`）仍在向 codex manifest（`openai_codex_models_service.go:298`）与 models 列表（`gateway_service.go:1616`）注入 `openai.DefaultModelIDs()`（任一无映射 OpenAI 账号即触发）。路由不受影响（eligibility 走 keys ∪ 快照），但列表膨胀 = “看得到、调不了”漂移的精确复刻。团队已书面暂缓（“M2 后续评估”），但 §11 验收“无空映射→平台默认”与之张力最大；发布前要么删除，要么在 release notes 明确划定范围。

### Minor
- **m-1（前端死代码，PR-D 策略选择）**：白名单模板块以 `v-if="false"` 保留（Edit 4 处：355/865/1096/1323；Create 3 处：2007/2494/2835；BulkEdit：323）+ 空 preset 循环（Create 1583/1816、Edit 1547）+ 死 handler（`addKiroPresetMapping`/`addAntigravityPresetMapping`：Create 5579/5595、Edit 4532）+ 空 computed 数组。功能安全（永不渲染），但三 modal 数百行死模板 + `allowedModels` 状态仍解析/提交（`buildModelMappingObject('combined', …)` 在空名单下恒等，Edit 3997）。建议后续整段删除而非 `v-if=false`。
- **m-2（死代码）**：`xai.DefaultModelMapping()`、`geminicli.GoogleOneModelMapping()` 零生产调用（计划 §6 要求删）及其单测保留；`ModelMappingRestricts` 写入端仍活跃（`credentialsBuilder.applyModelMappingRestricts`，Create 6124/Edit 5015 恒写 `false`；scheduler projection key `scheduler_cache.go:957`、persistence allowlist `account_credentials_persistence.go:38`），新账号永远携带死 flag。
- **m-3（管理 API 可写无效果字段）**：`admin_group.go:571,1104` + `group_handler.go:243,328` 仍接受/持久化 `model_allowlist`（含 `enabled=true`）。按“DB 列保留”决策是有意的，但 API 接受一个已无任何效果的 `enabled=true` 会造成运维困惑；建议 normalize 层直接忽略/拒绝 `enabled=true`，或文档化。
- **m-4（命名残留）**：`loadDefaultKiroModelMappings`（Edit 3548）现为空映射透传的 no-op，名字仍含 Default；`ModelRestrictionMode` 类型仍有 whitelist/combined 成员（已固定 'mapping'）。

### Nit
- **n-1**：`ModelAllowlistEnabled()` 注释仍称“开启后请求与列表受白名单约束”（`group_model_allowlist.go:83-84`），与恒放行实现矛盾。
- **n-2**：`AnnotateCatalogStorefrontCoverage` 注释称 coverage 是“fail-open”计数（`channel_storefront.go` 约 331 行），实现是证据型（无快照无映射不计）。注释误导。
- **n-3**：`openai_codex_models_service.go:55` 注释仍称 catalog “additionally restricted by FilterForListing”（已 no-op）；`getMappedModel` 注释“默认映射兜底”（`antigravity_gateway_service.go:291`）已无默认兜底。
- **n-4**：Select 修复本身正确且有回归测试（`b22cda7d5`，空 label → value 回退）；无问题，PASS。

## 4. 前 3 轮审计遗漏补充（本轮新发现，前 3 轮未提）
前 3 轮审的是计划、实现尚不存在，故以下均为新增：B-1/B-2（unit 套件未同步，M5 预警兑现）、B-3（node_modules 误提交）、M-1（转发层快照优先顺序偏离）、M-2（supplement 展示注入残留的定级）、m-1（`v-if=false` 死代码策略的具体清单）、m-2（写入端死 flag 仍活跃）、n-1〜n-3（注释与实现矛盾清单）。计划层面的 P1-P8/B1-B3/M1-M5 已落实，无需重审。

## 5. 测试运行结果（本轮实测）
| 命令 | 结果 |
|---|---|
| `go build ./...` | ✅ exit 0 |
| `go vet ./...` | ✅ exit 0 |
| `go test ./...`（无 tag） | ✅ 59 包 ok / 0 FAIL |
| `go test -tags unit ./internal/service/ -count=1` | ❌ FAIL（41 个测试；base 同命令 PASS） |
| `go vet -tags unit ./...` | ❌ 唯一失败：`internal/handler`（`filterUpstreamGeminiModelsBody` 未定义；base 通过） |
| `go test -tags unit ./internal/repository/` | ✅ ok |
| `go test -tags unit ./internal/handler/admin ./internal/handler/dto` | ✅ ok |
| `pnpm vitest run` | ✅ 298 文件 / 2102 测试通过 |
| `pnpm typecheck` | ✅ exit 0 |
| `pnpm check:i18n` | ✅ 3 passed |

## 6. 合并建议：需修复后合并
1. 修 B-1（unit 断言按新语义重写）与 B-2（gemini 测试同步），本地 `make test-unit` 全绿后再合（预计另需 1 个 fix commit）。
2. 同 commit 移除 `frontend/node_modules` symlink（B-3）。
3. M-1（映射优先重排 + 重叠场景单测）建议与 B-1 同批修复——它是 A→B→C 链路在 Antigravity 上的正确性问题，且与用户此前“-thinking 改写不生效”观感吻合。
4. M-2、m-1〜m-4、n-1〜n-4 可列为发布前 follow-up（M-2 建议进发布项，余下进清理项），不阻塞合并。
5. PR body 应修正验证口径：“59 包 ok”→ 明确为无 tag 套件；补上 `make test-unit` 结果后再标全绿。
