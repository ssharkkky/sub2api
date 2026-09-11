# Fork 模型约束与映射重构计划（零默认映射）

本文是 TokenSupply fork「模型约束 / 模型映射」重构的实施计划。它细化 `docs/MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` 第 3/5 步的产品基线，并加入一个更硬的决定：**零默认映射**。开发、审计、发布流程仍遵守 `docs/FORK_OPERATIONS_SOP.md`。

- 代码基线：v0.2.2-ts.1（tag `v0.2.2-ts.1`，`origin/main` = `36e19f93c`）。
- 本文是**计划**，尚未实现。每步一个功能 PR（不改 `VERSION`、不打 tag、不部署）。

## 0. 现状

- 生产（`sub2api-green`）运行 v0.2.2-ts.1。
- v0.2.2 引入的分组白名单 `model_allowlist` 把「展示」和「准入」绑成一个开关，与渠道定价（本 fork 的模型约束唯一来源）漂移，导致「列表看得到、调用 404」。
- **已做的一次性数据补救**（未改代码）：把生产 8 个启用白名单的分组 `model_allowlist.enabled` 全部置 `false`，恢复「渠道定价是唯一模型约束」。
- 本文是随后要做的**代码级长期重构**：删掉多余约束层、删掉所有默认映射、把映射改成纯显式、让渠道看到的是映射名。

## 1. 目标

1. 用户能看见、能调用的模型，**只由渠道决定**（`restrict_models` + `channel_model_pricing`）。
2. 账号映射、渠道映射**只负责改名**，不再当第二道约束/白名单。
3. **零默认映射**：不从任何来源（平台默认 / 模型目录 / 运行时选项）自动给账号注入映射；账号没有显式映射 = 透传。（Bedrock 别名是改名机制、非默认映射，保留，见 §3。）
4. 分组自定义模型列表（`model_allowlist`）**前后端彻底删除**。
5. 账号**原生模型定时同步**（`UpstreamCapabilitySyncService`）**保留**，用途：路由覆盖、能力检查、通配符展开；**不当用户货架**。
6. 渠道「看到的账号模型列表」= **映射名（mapping keys）**，不是账号原生名。

## 2. 四项范围

| 编号 | 范围 | 性质 |
|---|---|---|
| R1 | 保留账号原生模型定时同步（`UpstreamCapabilitySyncService` + `UpstreamModelSnapshot` + `HasSyncedUpstreamModel`） | 零改动，仅确保不被误删 |
| R2 | 前端删「自动配置映射 + 彩色快捷映射按钮」（antigravity 重灾区）+ 删账号白名单模式 UI + 后端默认映射注入/endpoint 按零默认处理 | 前端删除 + 后端决策 |
| R3 | 分组 `model_allowlist` 前后端彻底删除 | 后端 ~12 文件 + 前端 ~8 文件 + 测试；DB 列保留 |
| R4 | 渠道选择器展示的账号模型列表 = 映射名（非原生） | 后端 `channel_storefront.go` + 前端 `ChannelsView.vue` |

## 3. 关键决定：零默认映射

**定义**（区分两个概念）：
- **默认映射（删）** = 运维未显式配置时，系统**自动注入**到账号映射/公开名的东西（自动把账号的「可服务公开名集合」填满）。
- **账号别名（留）** = 账号把请求名改写成上游名的机制（如 Bedrock 的 `公开名 → us.anthropic.*` + region 前缀归一化）。它只做改名，**不决定账号对外提供哪些公开名**。
- **映射** = 账号上由运维**显式**配置的 `model_mapping`（公开名 → 上游名）。
- **透传 = 账号默认态**：除显式 `model_mapping` 覆盖的名字外，其余请求名原样转发。故账号可服务公开名集合 = **显式 mapping keys ∪ 原生快照**。

模型目录（`deploy/data/models.json`）仍可保存**参考数据**（已知上游改写、价格、结构），用于前端建议（手动应用）、价格查询、文档；但**绝不自动注入**到任何账号的映射（catalog 改写数据去留见 §12.3 开放问题）。

### 要删的默认映射（自动注入，5 层）

| # | 位置 | 现状 | 处理 |
|---|---|---|---|
| 1 | `account.go:639` `resolveStoredModelMapping` | 空映射时返回平台默认：`DefaultAntigravityModelMapping`(:643,663)、`DefaultKiroModelMapping`(:646,663)、`xai.DefaultModelMapping()`(:649,666,705)、`geminicli.GoogleOneModelMapping()`(:656,695) | 只返回显式存储；空则 `nil` |
| 2 | `platform_default_models.go:90` `applyCatalogDefaultMappings` | `restricts=false` 时注入 `modelcatalog.DefaultMappings(platform)`（`modelcatalog/catalog.go:406` 包级版，catalog 上游改写） | 删除/置空（不再自动合并 catalog 改写） |
| 3 | `account.go:756,762` `ensureAntigravityDefaultPassthroughs` + `applyAntigravityGemini31ProAliases`（调用点 :674-684） | 给 antigravity 显式映射增补 passthrough / 3.1-pro 别名 | 删除（显式映射原样使用） |
| 4 | `pkg/xai/models.go:150` `DefaultModelMapping()`；**账号注入点** `account.go:649,666,705`；**分组调度点** `openai_gateway_handler.go:209`→`Group.ResolveMessagesDispatchModel`；配置点 `setting_parse.go:1006` | grok 账号空映射自动注入整份默认映射（原生名+别名+跨客户端通配）；分组级 `claude-*`→`grok-4.6` 桥接 | **删账号注入**（空则 nil→透传）；`DefaultModelMapping()` 随之无调用方→删；**留**分组 dispatch + `ModelMappingWithOptions`/`RuntimeModelMappingOptions`/`SetRuntimeModelMappingOptions` + 设置项 `grok_default_text_model`/`grok_cross_client_model_map_enabled` |
| 5 | `pkg/geminicli/…` `GoogleOneModelMapping()`（引用点 `account.go:656,695`） | Gemini Google One 空映射默认 | 删除 |

> **第 4 层已拍板（B3/P3，见 §13.2）**：**删**账号级默认注入（`account.go:649,666,705`→`xai.DefaultModelMapping()`，空则 nil 透传）；**留**分组级调度改写（`openai_gateway_handler.go:209`→`Group.ResolveMessagesDispatchModel`，Grok 分组 `claude-*`→`grok-4.6`，由 `grok_cross_client_model_map_enabled` 控制）+ 其依赖 `ModelMappingWithOptions`/`RuntimeModelMappingOptions`/`SetRuntimeModelMappingOptions` + 设置项 `grok_default_text_model`/`grok_cross_client_model_map_enabled`。Kiro/GoogleOne/Bedrock 三类平台永无快照（见 §13.3-M1 平台覆盖表）。

### 要留的账号别名（改名机制，非默认映射）

| 位置 | 说明 |
|---|---|
| `bedrock_request.go:129` `normalizeBedrockModelID` 查 `domain.DefaultBedrockModelMapping`（`domain/constants.go`） | Bedrock **别名**改写（公开名 → `us.anthropic.*`）；**保留**（账号别名，不决定公开名列表，**无需 Bedrock 迁移**） |
| `bedrock_request.go:23-61` `AdjustBedrockModelRegionPrefix` / `BedrockCrossRegionPrefix` / `bedrockCrossRegionPrefixes` | Bedrock region 前缀归一化（跨区模型 ID 匹配账号 AWS Region）；保留（正确性） |
| `account.go` `isOpenAIOAuthServableModel` | OpenAI OAuth 空映射账号的厂商族守卫（#3662，防 400）；保留（防 400，非货架/默认映射） |

## 4. 发现的问题（必须处理）

### 4.1 无映射账号的路由（要改代码，否则路由错乱）

现状 `IsModelSupported`（`account.go:905`）对「无映射」账号返回 **true（全放行）**。零默认映射后大量账号变「无映射=透传」，于是任何公开名 P 都能路由到无映射账号 → 该账号把 P **原样透传** → 上游不认 → 400/failover。

必须改为（与「所有账号除模型映射外都透传」一致）：

```
IsModelSupported(model):
  OpenAI passthrough                    → true
  OpenAI OAuth 且无显式映射              → isOpenAIOAuthServableModel(model)   [保留厂商族守卫]
  model 命中显式 mapping keys（精确>通配） → true                                [模型映射：改写成 mapping 值]
  否则（透传）                             → HasSyncedUpstreamModel(model)       [原样转发；上游须原生支持]
```

即：账号可服务 **显式 mapping keys（改写）∪ 原生快照模型（透传）**；无映射账号 = 纯透传（原生）。与 R4 一致：选择器展示 mapping keys（映射账号）/ 原生快照（透传账号）。

配套：`isAccountModelSupportedForRequest`（`openai_gateway_scheduling.go:481`）与调度候选过滤保持一致。`HasSyncedUpstreamModel`（`account_upstream_capability.go:136`）无快照时 fail-open（:141-143）保留，避免失败探测把候选打空。

> **审计补充（见 §13.3）**：(M4) 现状 `IsModelSupported` 有四分支（passthrough 短路 / `!ModelMappingRestricts` / restricts 且空映射 / keys+二次查找），上伪代码漏前三分支，且 PR-A 删 flag 后三态如何坍缩需补全。 (M1) Kiro/Gemini-GoogleOne/Bedrock 三类平台永无快照，`HasSyncedUpstreamModel` 恒 fail-open，§4.1 需补「平台覆盖表」并为三类平台选定路由策略（接受恒放行 / 补同步覆盖 / 加平台守卫，三选一）。

### 4.2 存量账号数据迁移（生产，最高风险）

现在靠隐式**默认映射**跑着的账号（antigravity / kiro / grok / gemini），删默认映射后变「无映射=透传原生名」。而**渠道定价表里的公开名**（很多正是从默认映射 keys 来的）会**路由不到任何账号 → 404**。

**Bedrock 豁免**：其 `DefaultBedrockModelMapping` 是账号别名（保留），公开名仍会被正确改写，**无需 Bedrock 迁移**。

**迁移范围（P4/Codex 已拍板修订）**：不止空映射账号——**所有有效映射因删注入而变的账号**都要处理：
- **空映射账号**：删注入后丢整份平台默认 → 透传原生快照；
- **非空映射账号**：`applyCatalogDefaultMappings`（`platform_default_models.go:90`，restricts=false 时合并 catalog 改写）+ Antigravity 增补（`account.go:681-684` passthrough + 3.1-pro 别名）被删 → 丢这些合并项。**删注入对非空账号非零行为变化**。

**方法（P4）**：非空账号**保留原配置**（不按最新默认整表重算）；对每个受影响账号**对比迁移前后「路由资格 + 最终上游名」**，按差异**显式补齐**。「公开名在原生快照」**不**证明免迁（旧映射可能改道到别的目标）。

**时序（P7/Codex 已拍板修订）**：
1. 交付迁移工具 + **新旧行为对照验证**（逐账号 before/after 路由资格 + 上游名 diff，零风险只读）。
2. 必要时**完整固化**需兼容的当前有效映射（不只补渠道定价名——空→非空后旧代码 `resolveStoredModelMapping` 走存储分支会收窄丢项）。
3. 生产预迁移 + 验证（每个定价公开名仍路由到 ≥1 账号；**新旧两版都兼容**迁移后数据）。
4. 新代码发布（蓝绿）。**保留数据备份**；确认新旧两版都兼容后才承诺无需数据回滚。

**这步不做，上线即 404。** 参照 v0.2.2 白名单回滚的谨慎度执行。

### 4.3 R4 选择器精确定义 + coverage 语义

**已拍板（P1=∪）**：账号可服务公开名 = **显式 mapping keys ∪ 原生快照**（去重并集）。

- 账号公开模型列表 = **显式 mapping keys（通配 key 用账号原生快照展开成具体名）∪ 原生快照模型**（去重）。
- 渠道选择器（`ChannelsView.vue` catalogPicker）= 绑定账号上述列表的并集。
- **通配符 key 展开源（P2 已拍板）**：按**每个通配 key 是否有匹配**决定——先用账号原生快照展开（R1 保留同步的价值）；该 key 快照无匹配再回退平台 catalog（`PlatformDefaultModelIDs`）并标「回退」来源；catalog 也无匹配的跨客户端通配，保留**手填具体公开名**入口（不静默编造模型）。
- **coverage 语义（P2 已拍板）**：从「快照含该原生模型」改为「多少账号**可参与路由**此公开名（mapping keys 命中或原生快照命中，经实际路由判定含厂商守卫+映射目标检查）」。文案用「可参与路由」而非「已确认能服务」；`Synced` 字段仍表示有有效快照的账号数（≠ 确认支持数）。前端 `formatCatalogCoverage` 文案同步更新。

> **B1 已解决（P1=∪）/ P2 已拍板**：全文统一为 ∪（有映射账号的原生透传名也可选，与 §5 不变式一致）；通配展开/coverage/回退源按上（回退源用 `PlatformDefaultModelIDs`，与 P5 一致）。

## 5. 目标端到端流程（公开名 P、上游名 U）

```
用户 GET /v1/models  →  渠道货架(=定价公开名, restrict_models=true)  →  看到 P
用户请求 P
  → 渠道定价门禁: P ∈ channel_model_pricing ?   [唯一用户可见门禁, restrict_models]
  → 账号路由: 哪些账号能服务 P（P 命中该账号显式 mapping keys 精确>通配，或 P ∈ 该账号原生快照[透传]）
  → 选中账号: P 命中 mapping → 改写成 mapping 值 U；否则透传 U = P
  → 转发上游 U
  → 计费: billing_model_source=requested → 按 P（channel_mapped→渠道映射名 / upstream→U / response→响应模型）
```

不变式：**渠道定价公开名 ⊆ 绑定账号「可服务公开名」并集**（账号可服务 = 显式 mapping keys ∪ 原生快照；R4 选择器展示该并集，天然保证每个可定价公开名都路由得到）。

## 6. 清理项（不阻塞，但一并做干净）

- **`ModelMappingRestricts` flag 删除**（`account.go:884`）：新语义下账号可服务 = 显式 mapping keys ∪ 原生快照（透传为默认态），whitelist vs rename-only 的区分无意义；`applyCatalogDefaultMappings` 对 `restricts` 的判断（`platform_default_models.go:91`）随之删除。
- **前端白名单模式 UI 删除**：`modelRestrictionMode`、`allowedModels`（`EditAccountModal.vue`）；映射 UI = 纯手写 from→to 列表。
- **`xai.RuntimeModelMappingVersion` 缓存失效版本**（`account.go:600`）：grok 运行时映射删掉后变 no-op，可一并清理。
- **默认映射 endpoint 删除**：`GET /admin/accounts/antigravity/default-model-mapping`、`GET /admin/accounts/kiro/default-model-mapping`（`server/routes/admin.go:441-442`，`admin/account_handler.go:3400 GetAntigravityDefaultModelMapping` / `:3406 GetKiroDefaultModelMapping`）+ 前端 `fetchAntigravityDefaultMappings`/`fetchKiroDefaultMappings`。

## 7. 影响面清单（按 PR 分组）

### PR-A · 后端零默认映射 + 路由回退（R1 保留 + 第 3 节 5 层删除 + 4.1）
- `backend/internal/service/account.go`（`resolveStoredModelMapping`、`IsModelSupported`、`GetModelMapping`/`resolveModelMapping`、`ModelMappingRestricts`、antigravity 增补）
- `backend/internal/service/platform_default_models.go`（`applyCatalogDefaultMappings`）
- `backend/internal/pkg/xai/models.go`（`DefaultModelMapping`/`RuntimeModelMappingVersion`/`RuntimeModelMappingOptions`）
- `backend/internal/service/openai_messages_dispatch.go:75`、`setting_parse.go:1006`
- `backend/internal/service/bedrock_request.go`（Bedrock 别名 `DefaultBedrockModelMapping` + region 前缀均**保留**，无需改动；仅确认不被误删）
- `backend/internal/pkg/geminicli/…`（`GoogleOneModelMapping`）
- 测试：`backend/internal/domain/constants_test.go`、相关 service 测试

### PR-B · R4 渠道选择器 = 映射名
- `backend/internal/service/channel_storefront.go`（`storefrontModelUnion`:165、`storefrontModelsFromUnion`:190、`ListCatalogStorefrontModelsWithCoverage`:122、`AnnotateCatalogStorefrontCoverage`:267、`filterStorefrontCoverageAccounts`:229、通配展开；行号已按 §13.1 实测修正）
- `frontend/src/views/admin/ChannelsView.vue`（catalogPicker ~504-555、coverage 文案）
- `frontend/src/api/admin/channels.ts`（`CatalogStorefrontModel` 类型如需）

### PR-C · R3 分组 allowlist 删除
- 后端：`domain/model_allowlist.go`、`service/group_model_allowlist.go`、`service/admin_group.go`、`service/group.go`、`handler/gateway_handler.go`（Models/CodexModels 分支）、`handler/openai_gateway_handler.go:3731 blockedModelAllowlistCandidate`（调用点 :2376、:2808）、`service/openai_models_list.go`、`service/openai_codex_models_service.go`、`service/image_playground.go`、`ent/schema/group.go`（迁移 275 列保留）
- 前端：`GroupsView.vue`、`api/admin/groups.ts`、`types/index.ts`、i18n en/zh、`__tests__`（`modelAllowlistCandidates`、`GroupsView.columnSettings`、`GroupsView.duplicate`）
- 测试：`backend/internal/handler/gateway_models_test.go`（allowlist 语义用例）

### PR-D · R2 前端预设/白名单删除
- `frontend/src/composables/useModelWhitelist.ts`（`getPresetMappingsByPlatform`、各平台 `*PresetMappings`、`fetchAntigravityDefaultMappings`、`fetchKiroDefaultMappings`、`getModelsByPlatform`）
- `frontend/src/components/account/EditAccountModal.vue`、`BulkEditAccountModal.vue`、`CreateAccountModal.vue`（彩色按钮块、`addPresetMapping`、`addKiroPresetMapping`、`addAntigravityPresetMapping`、`modelRestrictionMode`、`allowedModels`）
- 测试：`useModelWhitelist.spec.ts`、`CreateAccountModal.spec.ts`、`BulkEditAccountModal.spec.ts`

## 8. 落地顺序

1. **生产只读排查 + 迁移工具**（4.2 步骤 1）：交付迁移工具，拉「受影响账号 × 渠道定价公开名 × before/after diff」清单，新旧行为对照验证。零风险。
2. **生产数据预迁移**（4.2 步骤 2-3）：必要时完整固化当前有效映射，逐账号补齐，验证新旧两版都兼容。保留数据备份。
3. **PR-A**（零默认映射 + 路由回退 + `mapAntigravityModel`）→ 本地全链路 + 按 SOP 审计合并。
4. **PR-B / PR-C / PR-D**（R4 选择器 / allowlist 删除 / 前端预设删除）→ 各自本地全链路 + 审计合并。
5. **发布（P6 已拍板）**：**推荐 A/B/C/D 分 PR 审核、同一次发布**（前端 `useModelWhitelist.ts:444,454` 调用 PR-A 删除的 endpoint，A 与 D 耦合）。若必须两车 → **A+D / B+C**（非 A / B+C+D），并验证第一车旧选择器在新路由语义下的行为。走 `FORK_RELEASE_WORKFLOW.md` 蓝绿。
6. **更新 SOP**：把「fork 删除/覆写清单」（第 3/6 节 + PR-A~D 触点）与本文「模型契约单源设计」立为未来同步的回归红线。

## 9. 与既有文档的关系

- **细化 `MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md`**：
  - 该文档第 3 步「账号映射只改名」+ 第 5 步「默认映射尽量读目录」→ 本文收紧为**零默认映射**：catalog 只留参考数据，映射恒为账号显式配置，不自动注入。
  - 该文档第 2 步「渠道从目录 + 同步上游模型勾选」→ 本文 R4 收紧为「渠道勾选的是**映射名**（映射账号）/原生快照（透传账号），catalog 提供价格/结构」。
  - 该文档第 4 步「定时同步上游能力」= 本文 R1（保留）。
  - `model_mapping_restricts` 语义：该文档第 3 步用它区分 whitelist/rename-only；本文零默认 + 映射恒过滤后，该 flag 可删除（见第 6 节）。
- **遵守 `FORK_OPERATIONS_SOP.md`**：一个 PR 一种主要行为；高风险路径（`migrations/`、调度、计费）审计结论以完整 `github.com/.../pull/<n>` 链接进 PR body。

## 10. fork 漂移风险（未来同步）

PR-A~D 全部踩在上游持续改动的文件上（`account.go` 映射解析、`xai` 运行时映射、`bedrock`、`modelcatalog`、默认映射 endpoint、allowlist、`channel_storefront`、前端账号/分组/渠道 UI）。**每次未来上游同步都会在这些文件冲突，需逐项 re-apply「删除/覆写」。**

处置：在第 8 节步骤 6 的 SOP 更新里，立一张「fork 删除/覆写清单」（本文第 3/6 节 + PR-A~D 触点 + 审计 PR 链接），同步时按表 re-apply 并留证据。

## 11. 验收标准

- 生产任一**已绑定渠道且 `restrict_models=true`** 的对外分组：`/v1/models` 返回集 = 渠道定价公开名（受限渠道空货架直接返回空列表 `gateway_handler.go:1206`，不误触发默认回退）；每个公开名都能真实调用成功（无 404/上游 400，迁移步骤 3 逐名验证）。
- 代码中不再存在任何「空映射 → 平台默认映射」的注入路径（5 层全删；Bedrock 别名保留）；`grep` 可验证。
- 账号可服务 = 显式 mapping keys ∪ 原生快照：无映射账号只路由原生快照内的模型；有映射账号路由 mapping keys 内的公开名（改写）+ 原生快照内的模型（透传）。
- 前端账号配置无彩色快捷按钮、无自动默认；分组配置无模型列表设置；渠道选择器展示映射名 + 覆盖率。
- 全链路：unit / integration / golangci-lint / 前端 vitest 全绿；审计 PR 链接齐全。
- 迁移后：存量对外分组公开名集合与迁移前一致（不无故变多变少）。

## 12. 已定问题（四项全部拍板，文档闭环）

1. **Bedrock = 账号别名，保留**：`DefaultBedrockModelMapping`（公开名 → `us.anthropic.*`）+ region 前缀归一化是**账号别名/改名机制**，不是默认映射，**保留不动**，**无需 Bedrock 迁移**。
2. **透传确认可接受**：无显式映射的账号（含 Gemini Google One / grok / antigravity / kiro）默认**透传原生名**，其原生名上游可直接接受，无需强制显式映射。
3. **catalog 参考数据：保留为建议（选 A）**：删掉 `modelcatalog.DefaultMappings`（catalog 上游改写 `{公开名: 上游名}`）的**自动注入**后，catalog 的 `upstream` 改写数据**保留为参考/建议**——前端账号编辑页将其作为建议展示（如「此模型上游通常是 X，应用？」），运维**手动应用**才写入账号 `model_mapping`，**不自动注入**。
   - 理由：(1) 建议只有运维点「应用」后才成为显式配置，与「映射恒为账号显式配置」一致，不违反零默认映射；(2) 降低运维手填负担、保持改写一致性；(3) 与 `MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md`「本仓库模型目录：我们知道的模型、档位、默认改名、底稿价」衔接（「默认改名」=「建议改名」）。
   - 落地：`applyCatalogDefaultMappings` 删除/置空（删自动注入）；前端账号映射编辑页加可选「应用上游建议」交互（读 `modelcatalog.DefaultMappings(platform)` 展示建议，手动应用才写入 `model_mapping`）。
4. **透传账号可进对外分组，无特殊限制**：透传是**所有账号的默认态**（「除模型映射外都透传」），透传账号的原生名正常进入 R4 并集，无需额外约束。

> **四项拍板完毕。** 但 2026-09-08 双 agent 审计（§13）发现需先修订，故实施入口从「§8 第 1 步」调整为「§13.6 修订后路径」。

## 13. 双 Agent 审计整合（Gemini + Opencode，2026-09-08）

### 13.0 审计元信息

- **审计方式**：paseo 同工作区（`wks_340784a7884fb0bd`，cwd=`/root/paseo/sub2api-sync-021`）并行开 2 个只读 agent，相同任务（5 角度：内部一致性 / 技术准确性 / 决定正确性 / 完整性与风险 / 落地顺序）审计本文档。
- **Agent 1（Gemini）**：`pi/ts-antigravity/gemini-3.8-flash-high`（thinking=max），报告 `docs/audit/FORK_MODEL_REFACTOR_PLAN.gemini.md`（6 发现）。
- **Agent 2（Opencode）**：`opencode/muse-spark-1.3-contributor-free`（mode=build，thinking=xhigh），报告 `docs/audit/FORK_MODEL_REFACTOR_PLAN.opencode.md`（15 发现）。
- **总体共识**（两份一致，置信度均高）：**方向正确，5 层删除清单主体准确，但文档不可直接开工 PR-A，需先修订。**

### 13.1 行号仲裁（Gemini vs Opencode 分歧，已实测裁定）

两个审计者对 `channel_storefront.go` 行号结论相反（Gemini 称 100% 吻合 :176，Opencode 称实际 :165）。**本维护者已逐一 `grep` 实测，裁定 Opencode 准确**，修正表如下（✓=文档原值正确）：

| 函数 | 文档原 | 实测 | 文件 |
|---|---|---|---|
| `storefrontModelUnion` | 176 | **165** | channel_storefront.go |
| `storefrontModelsFromUnion` | 196 | **190** | channel_storefront.go |
| `ListCatalogStorefrontModelsWithCoverage` | 117 | **122** | channel_storefront.go |
| `AnnotateCatalogStorefrontCoverage` | 267 | 267 ✓ | channel_storefront.go |
| `filterStorefrontCoverageAccounts` | 229 | 229 ✓ | channel_storefront.go |
| `isAccountModelSupportedForRequest` | 483 | **481** | openai_gateway_scheduling.go |
| `modelcatalog.DefaultMappings`（包级） | 411 | **406**（411 是方法版） | modelcatalog/catalog.go |
| `blockedModelAllowlistCandidate` | 3732 | **3731**（调用点 :2376、:2808） | openai_gateway_handler.go |
| `GetAntigravityDefaultModelMapping` | 3399 | **3400** | admin/account_handler.go |
| `GetKiroDefaultModelMapping` | 3405 | **3406** | admin/account_handler.go |
| `resolveStoredModelMapping`/`IsModelSupported`/`ModelMappingRestricts`/antigravity 增补/`applyCatalogDefaultMappings` | 639/905/884/756,762/90 | 全部 ✓ | account.go / platform_default_models.go |

> **基线 SHA 说明**：worktree HEAD `c36e5e96a`（squash 前 release commit）≠ origin/main `36e19f93c`（squash 后，带 #173）；代码同为 v0.2.2-ts.1 内容。实施时从 origin/main 开新分支，上表行号按实施分支重核。

### 13.2 阻断级发现（PR-A 前必须解决）

**B1 · R4 选择器语义自相矛盾（∪ vs XOR）**
- 矛盾：§3/§5/§11 用 **∪**（显式 mapping keys ∪ 原生快照）；§4.3 用 **XOR**（有显式映射→只展 keys，无→原生快照）。
- 后果：按 XOR 实现，「有映射账号的原生透传名」可路由（§5）却不可定价（§4.3 不展示）→ R4 保证不了 §5 不变式；按 ∪ 实现，§4.3 整段（含 coverage 口径）需重写。
- **建议修法**：全文统一为 **∪**（有映射账号展示 = mapping keys 展开 ∪ 原生快照的去重并集；与 §5/§11 一致）。**→ 已拍板 P1=∪（A）。**

**B2 · 核心调度路径遗漏 `mapAntigravityModel` + `gateway_scheduling.go`**
- 核心网关调度入口是 `gateway_scheduling.go:2774 isModelSupportedByAccountWithContext` / `:2831 isModelSupportedByAccount`（全文 10+ 调用点），不止 `openai_gateway_scheduling.go:481`。
- Antigravity 硬编码 `mapAntigravityModel(account, requestedModel)`（`antigravity_gateway_service.go:264`；调用点 `gateway_scheduling.go:2792`、`gateway_forward.go:1027`、`model_rate_limit.go:142`），内部调 `GetModelMapping()`。零默认映射后，空映射账号 `mapAntigravityModel` 若无快照回退会返回 `""` → `isModelSupportedByAccount` 返回 false → **透传被截断**。
- **建议修法**：PR-A 补 `mapAntigravityModel`（明确零映射下「快照优先 / 映射回退」与 §5「映射优先 / 快照回退」的顺序统一）+ `gateway_scheduling.go` 两处判定函数（`:2774 isModelSupportedByAccountWithContext` / `:2831 isModelSupportedByAccount`）；避免 Antigravity 路由被提前阻断。**→ 已拍板：补（加 `mapAntigravityModel` + `gateway_scheduling.go`）。**

**B3 · Grok dispatch 改写被误标为「账号默认映射」**
- `openai_messages_dispatch.go:75 xai.RuntimeModelMappingOptions()` 实为 **Grok 分组级 Claude-messages 调度改写**（`openai_messages_dispatch.go:71-80`：仅 `PlatformGrok` 且 claude 家族且 `EnableCrossClientMap` 时返回 `ModelMappingWithOptions(opts)["claude-*"]`；国产分组返回空 :82-87），**不是**账号 `model_mapping` 注入。
- 直接删 = Grok 分组失去 claude-* 调度目标（调度行为变更），非「删注入」。
- **建议修法**：第 4 层拆成 (a) 账号注入删除（`account.go` 三处 `xai.DefaultModelMapping()`）+ (b) Grok dispatch **保留**。**→ 已拍板 P3：留分组 dispatch（`openai_gateway_handler.go:209`→`Group.ResolveMessagesDispatchModel`，Grok 分组 `claude-*`→`grok-4.6`）+ 删账号注入（`account.go:649,666,705`→`xai.DefaultModelMapping()`，空则 nil 透传）；`ModelMappingWithOptions`/`RuntimeModelMappingOptions`/`SetRuntimeModelMappingOptions` + 设置项 `grok_default_text_model`/`grok_cross_client_model_map_enabled` 全留。路由不断裂：分组 dispatch 产出 `grok-4.6`（原生 xAI 模型，在快照内），后续账号透传判定通过。**

### 13.3 重要发现（应修）

**M1 · 三类平台永无快照，§4.1 修复对它们无效**（Opencode F-04）
- `supportsUpstreamModelSync`（`account_upstream_capability.go:204-215`）仅覆盖 Anthropic/OpenAI/Gemini/Grok/CN/Antigravity；**Kiro、Gemini-GoogleOne、Bedrock 永无快照**，`HasSyncedUpstreamModel` 恒 fail-open（:141-143）→ `IsModelSupported` 对新伪代码恒 true →「任意 P 路由到无映射账号 → 原样透传 → 上游 400」风险仍在。
- Bedrock「豁免、无需迁移」结论成立，但真正理由是「Bedrock 从未被映射门禁约束 + 永不同步快照」（`resolveStoredModelMapping` 对 Bedrock 全分支返回 nil → `:924` true），不只是「别名保留」。
- **建议修法**：§4.1 补「平台覆盖表」（哪些平台有快照 / 哪些恒 fail-open）+ 三类平台路由策略（接受恒放行 / 补同步覆盖 / 加平台守卫，三选一）。

**M2 · PR-C（R3 allowlist）影响面漏 ~10 处**（Gemini F-03 + Opencode F-07）
- 后端遗漏：`internal/server/middleware/group_model_allowlist.go`（请求体门禁中间件 + 测试）、`handler/batch_image_handler.go:147,154`、`handler/gemini_v1beta_handler.go:51-118`、`handler/dto/mappers.go:162` + `types.go:204`、`handler/admin/group_handler.go:243,328,628-630,739,894`、`service/api_key_auth_cache.go:111` + `api_key_auth_cache_impl.go:17`（缓存投影，删除后**缓存版本号需自增或清空策略**，防反序列化 panic）、`repository/*allowlist*` 投影测试、`routes/gateway_model_allowlist_test.go`、`openai_gateway_handler.go` 调用点 :2376/:2808。
- ent 生成代码（`ent/group*.go`、`ent/schema/group.go:270-271`、`ent/runtime`）不可手删，须 `entc` 重生成；**schema 删字段会生成删列迁移，与「DB 列保留」冲突**——需定制保留列的迁移写法，并与 SOP §5.4（只追加迁移）衔接。
- 前端遗漏：`views/admin/groupModelAllowlist.ts`、`views/admin/modelAllowlistCandidates.ts` 两个 helper 模块及 `__tests__/groupModelAllowlist*.spec.ts`、`modelAllowlistCandidates.spec.ts`。
- 另：`group_model_allowlist.go:34-45 supplementUnmappedOpenAIModels`（空映射 OpenAI 账号补 `openai.DefaultModelIDs()`）是另一个「空映射默认」，随 R3 删除，建议文档点名证明无遗漏。

**M3 · 迁移缺可执行工具 + 时序矛盾**（Gemini F-02 + Opencode F-06）
- §4.2 步骤 1/2/3 无排查 SQL/脚本、无写入通道（admin API？SQL？幂等？`Credentials==nil` 账号怎么写？写入值须严格等于当前有效默认目标）、无路由 dry-run 工具。
- 历史迁移 `migrations/058,059,060,071,144` 曾把当时默认回填进存储 → 存量「显式」mapping 混有**僵尸默认**，步骤 1「是否显式含此名」会误判为安全。**→ 拍板 P4。**
- 时序矛盾：§8（先合 PR-A）vs §4.2（补齐后再发布）。**建议**：生产数据**预迁移先行**（当前代码「显式映射优先于默认映射」，提前写显式映射对现有生产零破坏）→ 线上验证 → 代码 PR → 蓝绿发布。**→ 拍板 P7（迁移时序）。**

**M4 · `IsModelSupported` 现状描述不全，伪代码漏现存分支**（Opencode F-03）
- 实测现状四分支（`account.go:905-931`）：① `IsOpenAIPassthroughEnabled()` 短路 true（:910-912，issue #4936）；② `!ModelMappingRestricts()`（restricts=false）→ OAuth 无显式映射走守卫，否则 true（:913-918）；③ `ModelMappingRestricts()` 且空映射 → OAuth 守卫 / 否则 true「无映射=允许所有」（:919-925）；④ 命中 keys / `normalizeRequestedModelForLookup` 二次查找（:926-930）。
- 文档伪代码漏 ① passthrough 短路、② restricts=false 分支、④ 二次查找；且 §6 删 `ModelMappingRestricts` flag 后，`true/false/缺失` 三态如何坍缩未写。
- `isAccountModelSupportedForRequest`（`openai_gateway_scheduling.go:481-502`）的 `channelMapped` 分支**已**先查 `HasSyncedUpstreamModel`（:496-499），新 `IsModelSupported` 内部再查快照是双重检查（无害，但 PR-A 须写明是刻意）。
- **建议修法**：重写 §4.1——先完整列现状四分支，再给 flag 删除后的新分支表（含 passthrough 与 OAuth 守卫保留位置）。

**M5 · 测试面低估一个数量级；PR-B/C/D 测试清单缺失**（Opencode F-08）
- PR-A 强相关测试至少：`domain/constants_test.go`（默认映射断言全反转）、`service/antigravity_model_mapping_test.go`（「未配置用默认」用例反转）、`service/gateway_service_antigravity_whitelist_test.go:51,182-199`、`service/platform_default_models_test.go`、`service/account_wildcard_test.go`（xai 运行时 mock）、`service/openai_messages_dispatch_test.go`、`pkg/xai/models_test.go` + `oauth_test.go`、`pkg/geminicli/models_test.go`、`service/account_upstream_capability*.go` 单测（新路由语义）、`repository/scheduler_cache*.go`（`CredentialKeyModelMappingRestricts` 经 :957 进调度缓存 projection，删 flag 须同步）、`handler/admin/account_handler` 默认映射 endpoint 测试。
- PR-B（storefront 单测 + ChannelsView）、PR-C（`handler/gateway_models_test.go` 等，实际见 M2）、PR-D（`useModelWhitelist.spec.ts` 等 + 漏 `groupModelAllowlist*.spec`）均需补完整「必改/必增测试」表。

### 13.4 次要 / 建议

- **目标 1 张力**（Opencode F-09）：§1「调用只由渠道决定」与账号可服务门禁有张力——实际「可见 = 渠道定价；可调用 = 渠道定价 ∩ 账号可服务（keys ∪ 快照）」。文字修法，注明「快照门禁是正确性门禁（防 400/404），非政策白名单」。
- **「5 层」计数重复**（Opencode F-10）：层 1 已含 `geminicli.GoogleOneModelMapping()`（`pkg/geminicli/models.go:36`，被层 1 `account.go:656,695` 调用），层 5 又单列其定义。实为 **4 注入点（resolveStored / catalog / antigravity 增补 / xai 运行时）+ 2 待删定义（xai / geminicli）**。
- **R1「零改动」不成立**（Opencode F-13）：§4.1/§4.3/R4 依赖快照语义（`HasSyncedUpstreamModel` fail-open vs `SnapshotCoversModel` fail-closed :154-163 vs `snapshotCoversRequestedModel` :165-187），R1 至少有「语义澄清 + 平台覆盖表 + R4 侧改动」三项。改述为「同步机制不动；快照语义澄清并冻结为契约」。
- **验收标准 §11 三项不可执行**（Opencode F-14）：grep 需精确模式 + 允许列表（naive `grep Default.*Mapping` 会命中 Bedrock 别名 `domain/constants.go:188`、region 前缀、历史迁移、测试残留）；「集合一致」需逐分组 `/v1/models` 快照 diff + 路由 dry-run；「无 404」需界定观测窗口与抽样。
- **Claude OAuth/Vertex 长短名**（Gemini F-04）：`gateway_scheduling.go:2822-2824` + `gateway_forward.go:294-302` 对非 APIKey Claude 账号自动 `claude.NormalizeModelID` / `normalizeVertexAnthropicModelID`（`pkg/claude/constants.go:230-240`）长短名/Vertex 改写。应列入「保留的底层协议别名」（与 Bedrock 并列）。**→ 拍板 P8。**
- **R2 定义含糊、PR 边界不清**（Opencode F-12）：R2 后端部分归 PR-A（含 endpoint 删除），前端部分归 PR-D；账号级 whitelist UI（PR-D）与分组级 allowlist UI（PR-C）分界文件需注明（共用 `useModelWhitelist.ts:552-584 buildModelMappingObject`）。
- **§8 缺四步 + 发布粒度**（Opencode F-15）：缺 (a) 迁移工具 PR、(b) `MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` 第 3/5 步同步修订任务、(c) 每 PR 的 SOP 门禁映射（PR-A 踩调度+计费、PR-C 踩 migrations/ent → 审计结论链接进 PR body）、(d) 回滚声明（显式化后旧版读显式行为不变，回滚只需切镜像、无需数据回滚——应写为迁移安全论据）。B/C/D 发布粒度见 P6。

### 13.5 待拍板项（合并 8 项）

| # | 级别 | 问题 | 建议 | 状态 |
|---|---|---|---|---|
| **P1** | 阻断 | R4 选择器 **∪**（keys ∪ 快照）还是 XOR（有映射只展 keys）？ | **∪** | **已拍板：∪** |
| **P2** | 阻断 | R4 精确定义：通配展开源 / coverage 口径 / catalog 回退源 | 通配按 key 匹配展开（快照→`PlatformDefaultModelIDs` 回退+警告→手填）；coverage=「可参与路由」(fail-open，经实际路由判定)；回退源=`PlatformDefaultModelIDs` | **已拍板**（Codex 修正后） |
| **P3** | 重要 | Grok dispatch 改写（`openai_messages_dispatch.go:71-80`）+ 2 设置项（`settings_view.go:219-220`）保留还是删除？ | **保留**（最小 blast radius） | **已拍板：留分组 dispatch + 删账号注入** |
| **P4** | 重要 | 僵尸默认：免迁还是重算？ | 非空保留原配置；迁移范围=所有有效映射因删注入而变的账号；逐账号 before/after diff 补齐 | **已拍板**（Codex 修正：非空也受影响） |
| **P5** | 重要 | 未绑渠道 `/v1/models` 回落 + `PlatformDefaultModelIDs` 去留；composite 特判 | 留回落 + 留 `PlatformDefaultModelIDs`；composite 按平台算再并集；§11 限定 bound+restrict_models=true | **已拍板** |
| **P6** | 建议 | B/C/D 发布粒度 | **A/B/C/D 分 PR 审核、同一次发布**；若两车→A+D / B+C（前端调 A 删的 endpoint） | **已拍板**（Codex 修正） |
| **P7** | 重要 | 迁移时序 | 迁移工具+新旧对照→必要时完整固化→预迁移+验证→发布→留备份；确认新旧都兼容才承诺无数据回滚 | **已拍板**（Codex 修正） |
| **P8** | 重要 | Claude 长短名归类 | 列入保留的底层协议别名（同 Bedrock）；统一调度/转发顺序需再核 | **已拍板** |

### 13.6 修订后实施路径（吸收审计后）

1. **拍板 P1–P8 全部完成**（P1=∪、P2/P4/P6/P7 经 Codex 第三轮审计修正、P3/P5/P8 已拍板；B1/B2/B3 已拍板）。见 §13.7。
2. **修文档**：统一 ∪（B1/P1）、补 `mapAntigravityModel`+`gateway_scheduling.go`（B2）、拆 Grok dispatch（B3/P3）、平台覆盖表（M1）、PR-C 影响面补全（M2）、迁移工具 + 时序（M3/P7）、`IsModelSupported` 现状四分支重写（M4）、测试表（M5）、行号按 §13.1 批量修、目标 1 文字修（F-09）、R1 改述（F-13）、§11 可执行化（F-14）、§8 补四步（F-15/P6）、Claude 别名归类（P8）。
3. **交付迁移工具**（M3）：只读排查 SQL/脚本 + 写入 runbook + 路由 dry-run；跑 §8 第 1 步，拿「依赖默认映射的账号 × 定价公开名」清单与规模。
4. **按 §8 执行**：PR-A → 数据预迁移 → B/C/D（同 train）→ 蓝绿发布 → SOP + 产品基线双文档更新；PR-A/PR-C 附 SOP §7 审计结论链接。
5. **实施分支**从 origin/main（`36e19f93c`）新开，行号按 §13.1 表重核。

### 13.7 Codex 第三轮审计（6 项拍板，2026-09-08）

- **审计方式**：paseo codex agent（`codex/gpt-6-astra`，full-access，thinking=high，agentId `d674b1a2-6193-4c38-9b4d-daccf735ed97`）同工作区只读审计 §13.5 的 6 个待拍板项（P2/P4/P5/P6/P7/P8），逐项给「同意/不同意/需细化」。
- **结论**：4 项修正（P2/P4/P6/P7，均经本维护者 `grep`/读码验证成立）、2 项同意（P5/P8）。全部 6 项已按修订推荐拍板。

| 项 | Codex 意见 | 验证 | 拍板 |
|---|---|---|---|
| P2 | 需细化：(a) 按 key 匹配决定回退；(b) coverage=「可参与路由」非「已确认」；(c) `PlatformDefaultModelIDs` 是 `PublicIDs` 超集（`platform_default_models.go:24`），原「PublicIDs 更全」说反 | ✔ :24 `mergeUniqueModelIDs(packageDefault, PublicIDs)` | 按 Codex 修正 |
| P4 | 不同意免迁：非空账号仍被 `applyCatalogDefaultMappings`(:90) + Antigravity 增补(:681-684) 运行时增补，删注入非零变化 | ✔ :90-117 对非空也合并；:681-684 作用于非空 | 迁移范围=所有有效映射因删注入而变的账号；逐账号 before/after diff |
| P5 | 同意 + §11 限定 bound+restrict_models=true、受限空货架返回空(:1206) | ✔ | 按推荐 |
| P6 | 不同意 A / B+C+D：前端 `useModelWhitelist.ts:444,454` 调 A 删的 endpoint | ✔ :444 import + :454 调用 | A/B/C/D 分 PR 审核、同车发布；两车则 A+D / B+C |
| P7 | 需细化：空→非空后旧代码走存储分支收窄丢项，非天然零破坏 | ✔ `account.go:671-685` 非空走存储 | 迁移工具+新旧对照→必要时完整固化→预迁移→发布→留备份 |
| P8 | 同意 + 调度/转发顺序不一致需再核（`gateway_scheduling.go:2850` 先归一化 vs `gateway_forward.go:289` 先匹配显式映射） | 待核 | 列入保留协议别名；顺序差异列入 PR-A 待核 |

- **Codex 最高风险 Top-3**（已采纳进 §4.2/§8/§11）：① P4/P7 迁移兼容性；② P2 coverage 可信度（fail-open ≠ 保证成功）；③ P6 A/D 发布依赖。

## 14. 实施完成状态（2026-09-11，分支 `codex/model-refactor`）

**全部 4 个 PR 已完成并提交**（9 commits，66 文件，+500/-3743，净删 3243 行）：

| PR | 范围 | Commits | 状态 |
|---|---|---|---|
| Select 修复 | `Select.vue` 空标签回退 option value | `b22cda7d5` | ✅ |
| PR-A | 后端零默认映射 + `IsModelSupported` 4 分支 + 路由回退 | `67579c49d` | ✅ |
| PR-B | 渠道 picker union = mapping keys ∪ 快照 + coverage 证据型 | `8f5d7b7b7` | ✅ |
| PR-C | 分组 allowlist 门禁/过滤/UI 删除（middleware+service+handler+frontend） | `bf24bce92`+`84ff29e1e`+`e16abfd7d` | ✅ |
| PR-D | 前端预设/白名单删除 + 后端默认映射 endpoint 删除 | `cfeb97689`+`bcaf4f16b`+`6fcd81d2c` | ✅ |

**验证**：
- 后端：`go build` + `go vet` + `go test` 59 包 ok / 0 FAIL。
- 前端：`vitest` 298 文件 / 2102 测试 + `typecheck` + `check:i18n` + `build` 全通过。

**零默认映射不变式落地**：
- 映射恒为账号显式 `model_mapping`；无显式映射 = 透传。
- 账号可服务公开名集合 = 显式 mapping keys ∪ 原生快照。
- 渠道定价公开名 ⊆ 绑定账号可服务公开名并集（渠道定价唯一约束）。
- 分组白名单 / 账号白名单不再是独立用户可见约束源。
- Bedrock = 账号别名（留）；catalog `upstream` 改写 = 建议/参考（前端手动应用）。

**待办**（发布前）：
- 本地"整台 sub2api"测试环境搭建 + 全链路冒烟（A→B→C）。
- 走 `FORK_RELEASE_WORKFLOW.md` 蓝绿发布；发布前后各跑 `verify_coverage.sql`。
- 更新 SOP「fork 删除/覆写清单」。
