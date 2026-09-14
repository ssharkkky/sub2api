# 审计报告：FORK_MODEL_REFACTOR_PLAN.md（零默认映射重构计划）

- 被审文档：`docs/FORK_MODEL_REFACTOR_PLAN.md`（192 行，文档自称约 205 行）
- 基线文档：`docs/MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md`（219 行）、`docs/FORK_OPERATIONS_SOP.md`
- 代码基线：worktree `VERSION = 0.2.2-ts.1`，HEAD `c36e5e96a`，tag `v0.2.2-ts.1` 存在
- 审计方式：只读；所有 file:line 均在本 worktree 实测（行号以当前 HEAD 为准）
- 审计日期：2026-09-08

## 总体结论

**该文档需修订后实施，不可直接开工 PR-A。置信度：高。**

方向正确（零默认映射 + 透传默认态 + R4 映射名选择器与“渠道定价是唯一模型约束”一致），5 层删除清单主体准确；但存在 1 处核心语义矛盾（可服务集 ∪ vs 选择器 XOR）、1 处机制混淆（Grok 组调度改写被当成账号默认映射）、多处行号漂移与影响面遗漏、以及迁移工具与 R4 精确定义缺失。补完发现清单 F-01～F-04（阻断/重要）后方可进入 §8 第 1 步。

---

## 发现清单

### F-01［阻断］§3/§5/§11 的“并集”与 §4.3 的“二选一”互相矛盾，R4 不可实施

- 问题：文档对“账号可服务集”与“选择器展示集”用了两套互斥定义：
  - §3:39 定义账号可服务公开名集合 = **显式 mapping keys ∪ 原生快照**；§5:109-110 路由（命中改写、否则透传查快照）与 §5:115 不变式（定价 ⊆ 并集）、§11（有映射账号路由 keys 改写 + 快照透传）都是 ∪ 语义。
  - §4.3:98 却定义账号公开模型列表 = **有显式映射 → mapping keys / 无显式映射 → 原生快照**（XOR：有映射账号的原生透传名不进选择器）。
  - 若按 §4.3 实现，有映射账号的原生名无法被定价，但按 §5 它又可路由 → R4 选择器保证不了 §5 不变式；若按 ∪ 实现，§4.3 整段（含 coverage 口径）要重写。
- 证据：`docs/FORK_MODEL_REFACTOR_PLAN.md` §3:39、§4.3:98、§5:109-115、§11:178。
- 建议修法：二选一并全文统一（推荐 ∪：有映射账号展示 keys 展开 + 快照原生名的去重并集，与 §5/§11 一致；§4.3 重写为并集定义）。此项即拍板项 P1。

### F-02［阻断］第 4 层把“Grok 分组调度改写”误归为“账号默认映射”，删法不可执行

- 问题：§3 表格第 4 行把 `openai_messages_dispatch.go:75` 列为 xai 运行时默认映射的“应用点”，要求随运行时映射一并删除。但实测该处是**分组级 Claude-messages 调度改写**，不是账号 `model_mapping` 注入：
  - `backend/internal/service/openai_messages_dispatch.go:71-80`：仅当 `g.Platform == PlatformGrok` 且请求是 claude 家族且 `EnableCrossClientMap` 才返回 `ModelMappingWithOptions(opts)["claude-*"]`；国产分组直接返回空（:82-87）。
  - 删掉它 = Grok 分组失去 claude-* 调度目标（返回空），属于调度行为变更，不是“删注入”。文档未定义替代行为（返回空是否可接受？Grok + Claude 客户端路径是否就此中断？）。
  - 连带未决策：`setting_parse.go:1006-1009` 的写入点删后，`settings_view.go:219-220,414-415` 的 `grok_default_text_model / grok_cross_client_model_map_enabled` 设置项与 `domain_constants.go:506-511` 的 SettingKey 去留未定。
- 证据：`backend/internal/service/openai_messages_dispatch.go:62-80`、`backend/internal/service/setting_parse.go:1005-1009`、`backend/internal/pkg/xai/models.go:150-198`。
- 建议修法：第 4 层拆成两项：(a) 账号注入删除（`account.go` 三处 `xai.DefaultModelMapping()` + `RuntimeModelMappingVersion` 缓存键）；(b) Grok 分组 dispatch 改写**单独决策**（保留/改默认值/删除并说明调用方影响），设置项去留一并拍板（拍板项 P3）。

### F-03［重要］§4.1 的 `IsModelSupported` 现状描述不完整，伪代码遗漏三个现存分支，不可直接实施

- 问题：
  1. “无映射返回 true”只在 `GetModelMapping()` 为空**且**非 passthrough、且 `ModelMappingRestricts()` 为 true 分支内成立（`account.go:919-925`）；而 `ModelMappingRestricts()=false` 的新账号同样直接 true（`:913-918`），`Credentials==nil` 的账号根本到不了“无映射”（走默认分支 `:640-653`）。现状比文档描述多两个放行路径。
  2. 伪代码（§4.1:69-75）遗漏：`IsOpenAIPassthroughEnabled()` 短路（`:910-912`）、`normalizeRequestedModelForLookup` 二次查找（`:929-930`，Gemini/Antigravity `gemini-3.1-pro-preview-customtools` 等）、`ModelMappingRestricts` flag 删除后的分支重组（§6 删 flag，但伪代码没写 flag 删除后 `true/false/缺失` 三态如何坍缩）。
  3. `isAccountModelSupportedForRequest`（`openai_gateway_scheduling.go:481-502`）的 `channelMapped` 分支**已经**先查 `HasSyncedUpstreamModel`（:496-499），新 `IsModelSupported` 若内部再查快照则是双重检查——无害但 PR-A 必须写明，避免评审时被当成重复逻辑。
- 证据：`backend/internal/service/account.go:905-931`、`backend/internal/service/openai_gateway_scheduling.go:481-502`。
- 建议修法：重写 §4.1：先完整列出现状四分支（passthrough / restricts=false / OAuth 守卫 / 白名单+空映射放行），再给 flag 删除后的新分支表（含 passthrough 与 OAuth 守卫的保留位置），并注明 scheduling 双重检查是刻意为之。

### F-04［重要］§4.1 修复对三类平台无效：Kiro / Gemini-GoogleOne / Bedrock 不在快照同步覆盖内，fail-open 使门禁恒为 true

- 问题：`HasSyncedUpstreamModel` 无快照时 fail-open（`account_upstream_capability.go:141-143` return true，文档引用正确）。但 `supportsUpstreamModelSync`（`:204-215`）仅覆盖 Anthropic / OpenAI / Gemini / Grok / CN / Antigravity——**Kiro、Gemini Google One、Bedrock 永无快照**，删默认映射后这些平台 `IsModelSupported` 对新伪代码恒为 true，§4.1 担心的“任意 P 路由到无映射账号 → 原样透传 → 上游 400”对这些平台**依然存在**，文档未分析此缺口。
  - Bedrock 现状本就恒 true（`resolveStoredModelMapping` 对 Bedrock 全分支返回 nil → `GetModelMapping` 空 → `:924` true，别名在 `normalizeBedrockModelID` 另行处理），所以“Bedrock 豁免、无需迁移”结论成立，但理由不完整：豁免的真正原因是“Bedrock 从未被映射门禁约束 + 永不同步快照”，而不只是“别名保留”。
  - 另有未验证假设：若将来 Bedrock 开始同步快照（快照存 `us.anthropic.*` 原生名），零默认后无显式映射的 `GetMappedModel` 返回原名（如 `claude-opus-5`），`snapshotCoversRequestedModel`（`:165-187` 经 `GetMappedModel` 查快照）将查不到 → 误拒。当前因不同步而“碰巧通过”，文档应写明该假设。
- 证据：`backend/internal/service/account_upstream_capability.go:136-149,165-187,204-215`、`backend/internal/service/account.go:651-652,668,707,919-925`、`backend/internal/service/bedrock_request.go:124-139`。
- 建议修法：§4.1 补“平台覆盖表”（哪些平台有快照、哪些恒 fail-open），并给出三类平台的路由策略（接受恒放行 / 补同步覆盖 / 加平台守卫三选一）；Bedrock 豁免理由补全。

### F-05［重要］R4 通配展开、coverage 口径、catalog 回退三处未定义

- 问题：
  1. 通配 key 展开源：§4.3 指定“账号原生快照”，但跨客户端通配（如 Grok 遗留的 `gpt-*`/`claude-*`，运行时映射删除后本就消失）与快照无交集时展开为空，规则未写；快照为空（fail-open 账号）的通配 key 如何展示也未写。
  2. coverage 口径：`AnnotateCatalogStorefrontCoverage`（`channel_storefront.go:267-304`）现状用 `snapshotCoversRequestedModel`（经 `GetMappedModel` 间接感知映射）统计已同步账号、 total 计全量。文档只说改成“多少账号含此公开名”，未说明 unsynced 账号按 fail-open 计入还是 fail-closed 不计——两种口径差很大。
  3. “快照未命中回退平台 catalog 并标警告”：回退源是 `PlatformDefaultModelIDs`（手写默认 ∪ 目录 ID，`platform_default_models.go:20-35`）还是 `modelcatalog.PublicIDs / StorefrontItems`？“警告”形态（字段？文案？`formatCatalogCoverage` 怎么写？）未定义。
  4. `mapAntigravityModel`（`antigravity_gateway_service.go:272-291`）是“快照优先、映射回退”，与 §5 新路由“映射优先、快照回退”顺序相反，PR-A/PR-B 必须统一或论证。
- 证据：`backend/internal/service/channel_storefront.go:122-149,165-227,267-304`、`backend/internal/service/antigravity_gateway_service.go:265-291`、`frontend/src/views/admin/ChannelsView.vue:1131-1148`（`formatCatalogCoverage*`）。
- 建议修法：R4 补精确定义（含通配展开算法、coverage 分子分母、回退源与警告字段），并声明 antigravity 转发顺序是否同步改。拍板项 P2。

### F-06［重要］§4.2 数据迁移缺可执行工具：排查查询、写入通道、验证手段全未定义；历史迁移回填的“僵尸默认”无判定规则

- 问题：
  1. 步骤 1“只读拉清单”需跨 group（`restrict_models`）→ channel 定价 → 绑定账号 → 账号**有效**映射（含通配命中、catalog 注入、xai 运行时选项）四表联查，无现成查询/脚本。
  2. 步骤 2“显式写入 `model_mapping`”无写入通道（admin API 逐个写？SQL？幂等？`Credentials==nil` 账号怎么写？写入值是否严格等于当前有效默认目标？——只有严格等于，SOP“功能 PR 不部署”下的“旧版行为不变”才成立，文档隐含但未明说）。
  3. 历史迁移 `migrations/058,059,060,071,144` 曾把当时默认回填进存储：存量“显式”mapping 里混有僵尸默认，步骤 1 的“是否显式含此名”会误判为安全。视为显式（免迁）还是一律重算，需拍板（P4）。
  4. 步骤 3“逐分组验证路由”无 dry-run 工具（希望：给定分组+公开名，输出候选账号判定过程）。
- 证据：`backend/migrations/058_add_sonnet46_to_model_mapping.sql`、`071_*`、`060_*`、`051_*`、`059_*`、`144_add_opus48_to_model_mapping.sql`；`docs/FORK_MODEL_REFACTOR_PLAN.md` §4.2:87-94。
- 建议修法：§8 第 1 步前加“迁移工具 PR”（只读排查 SQL/脚本 + 写入 runbook + 路由 dry-run 查询），并明确写入值 = 当前有效默认目标、僵尸默认判定规则。

### F-07［重要］R3（PR-C）影响面清单遗漏约 10 处，后端中间件/网关/DTO 与前端 helper 全未列

- 问题：文档 PR-C 后端列 10 文件、前端列 4 项，但实测 allowlist 触点更多：
  - 后端遗漏：`internal/server/middleware/group_model_allowlist.go`（请求体门禁中间件）及其测试、`internal/handler/batch_image_handler.go:147,154`（图片列表过滤）、`internal/handler/gemini_v1beta_handler.go:51-118`（上游 Gemini 列表过滤）、`internal/handler/dto/mappers.go:162` + `types.go:204`、`internal/handler/admin/group_handler.go:243,328,628-630,739,894`（`GetGroupModelAllowlistCandidates` 等管理端 CRUD）、`internal/service/api_key_auth_cache.go:111`（缓存投影）、`internal/repository/*allowlist*` 投影测试、`internal/server/routes/gateway_model_allowlist_test.go`。`openai_gateway_handler.go` 除 `:3731` 定义外，调用点 `:2376,:2808` 未列。
  - 前端遗漏：`views/admin/groupModelAllowlist.ts`、`views/admin/modelAllowlistCandidates.ts` 两个 helper 模块及其 `__tests__/groupModelAllowlist*.spec.ts`、`modelAllowlistCandidates.spec.ts`；`GroupsView.duplicate.spec.ts` / `columnSettings.spec.ts` 只列了后两者名、漏了前两个 helper 的测试。
  - ent 生成文件（`ent/group*.go`、`ent/schema/group.go:270-271`、`ent/runtime`）不可手删，须 `entc` 重生成；schema 字段删除会生成删列迁移，与“DB 列保留”冲突——需定制保留列的迁移写法，文档只写“迁移 275 列保留”一句，不可执行。且与 SOP §5.4（只能追加迁移、禁改基线迁移）如何衔接未写。
  - 另：`group_model_allowlist.go:34-45` `supplementUnmappedOpenAIModels`（空映射 OpenAI 账号补 `openai.DefaultModelIDs()`）是另一个“空映射默认”，随 R3 删除是顺带解决，但文档未点名——建议显式列入，证明无遗漏。
- 证据：上列 file:line；`backend/ent/schema/group.go:270-271`；`backend/migrations/275_group_model_allowlist.sql`；`backend/internal/service/group_model_allowlist.go:14-45`。
- 建议修法：重写 PR-C 清单（含中间件/网关/DTO/缓存/ent-codegen/前端 helper），并给出 ent 删字段留列的迁移做法。

### F-08［重要］PR-A 测试面被低估一个数量级；PR-B/C/D 的测试清单缺失

- 问题：PR-A 只列“`domain/constants_test.go`、相关 service 测试”。实测强相关测试至少：`domain/constants_test.go`（默认映射断言全反转）、`service/antigravity_model_mapping_test.go`（“未配置用默认”用例反转）、`service/gateway_service_antigravity_whitelist_test.go:51,182-199`、`service/platform_default_models_test.go`、`service/account_wildcard_test.go`（xai 运行时 mock）、`service/openai_messages_dispatch_test.go`、`pkg/xai/models_test.go` + `oauth_test.go`、`pkg/geminicli/models_test.go`、`service/account_upstream_capability*.go` 单测（新路由语义）、`repository/scheduler_cache*.go`（`CredentialKeyModelMappingRestricts` 经 `:957` 进调度缓存 projection，删 flag 须同步）、`handler/admin/account_handler` 默认映射 endpoint 测试。PR-B（storefront 单测 + ChannelsView）、PR-C（`handler/gateway_models_test.go` 等 allowlist 语义用例——文档只列了这一个，实际见 F-07）、PR-D（`useModelWhitelist.spec.ts`、`CreateAccountModal.spec.ts`、`BulkEditAccountModal.spec.ts` 列了，但 `groupModelAllowlist*.spec` 漏了）均无完整清单。
- 建议修法：每 PR 补“必改/必增测试文件”表；与 SOP L1/L2 对应（unit → integration → lint/vitest 全绿才合）。

### F-09［重要］§1 目标 1 与基线产品规则存在张力：“调用只由渠道决定”不成立

- 问题：§1:17 “用户能看见、能调用的模型，只由渠道决定（`restrict_models` + `channel_model_pricing`）”。但 §4.1/§5 给调用加了第二道门（账号可服务门禁：keys ∪ 快照），调用实际 = 渠道定价 ∩ 账号能力。这与基线 `MODEL_CATALOG…md` §2 规则 3“分组和账号不再限制模型”也有表面冲突（快照门禁是一种账号限制，虽出于正确性而非政策）。
- 建议修法：目标 1 拆成两句——“可见 = 渠道定价；可调用 = 渠道定价 ∩ 账号可服务（keys ∪ 快照）”，并加注“快照门禁是正确性门禁（防 400/404），不是政策白名单”，与基线规则 3 的关系写明。此为文字修法，不影响技术方向。

### F-10［次要］“5 层”计数重复：层 1 已含 Gemini，层 5 是同一引用的定义

- 问题：§3 表层 1 已列 `geminicli.GoogleOneModelMapping()`（`account.go:656,695`），层 5 又把同一函数的定义单独计一层。真实注入点是 4 处（resolveStored / catalog / antigravity 增补 / xai 运行时），geminicli 只是被层 1 调用的定义（`pkg/geminicli/models.go:36`）。
- 建议修法：改称“4 个注入点 + 2 个待删定义（xai / geminicli）”，或保留 5 行表格但注明层 5 = 层 1 引用的定义。

### F-11［次要］R4 与 §2/§7/§8 的 file:line 系统性漂移（5～11 行），须批量修正

- `storefrontModelUnion` 文档 :176 → 实际 :165；`storefrontModelsFromUnion` :196 → :190；`ListCatalogStorefrontModelsWithCoverage` :117 → :122；`AnnotateCatalogStorefrontCoverage` :267 ✓；`filterStorefrontCoverageAccounts` :229 ✓。
- `modelcatalog/catalog.go:411` → 包级 `DefaultMappings` 实际 :406（:411 是 `(Catalog)` 方法版；`applyCatalogDefaultMappings` 调的是包级版 `:94 → :406`，建议写 406）。
- `openai_gateway_scheduling.go:483` → 函数实际 :481（体 :481-502）。
- `account.go` antigravity 增补调用点 `:674-684` → 实际 `:679` 与 `:689`（范围漏 :689）。
- `admin/account_handler.go:3399-3405` → 函数实际 `:3400-3408`。
- `openai_gateway_handler.go:3732` → 函数实际 `:3731`（调用点另有 :2376、:2808）。
- 第二段默认 `DefaultAntigravityModelMapping(:643,663)` → 第二次出现实际是 `:660`（`:663` 是 Kiro 第二次出现）。
- 建议修法：按上表批量改行号；长久之计行号后加函数名（已多处有，好习惯，保持）。

### F-12［次要］§2 R2 定义含糊，R2/PR-A/PR-D 边界不清

- 问题：R2 = “前端删预设 + 删账号白名单 UI + 后端默认映射注入/endpoint 按零默认处理”。其中“后端默认映射注入”正是 PR-A 的第 3 节 5 层删除，“endpoint”在 §6。落地 §8 把 PR-A 与 PR-D 分开，但 R2 横跨两者——R2 到底在哪个 PR 验收？`EditAccountModal.vue` 等三处 `modelRestrictionMode/allowedModels`（如 `CreateAccountModal.vue:4857-4858`）既是 R2 又是白名单 UI，与 PR-C 的 allowlist 删除也有重叠（账号级 vs 分组级白名单 UI 共用 `useModelWhitelist.ts:552-584` 的 `buildModelMappingObject`）。
- 建议修法：明确“R2 后端部分归 PR-A（含 endpoint 删除），R2 前端部分归 PR-D”，并注明账号级 whitelist UI（PR-D）与分组级 allowlist UI（PR-C）的分界文件。

### F-13［次要］R1 “零改动”不成立：快照语义澄清本身就是改动

- 问题：§2 R1 = “零改动，仅确保不被误删”。但 §4.1/§4.3/R4 深度依赖快照语义三选一（`HasSyncedUpstreamModel` fail-open vs `SnapshotCoversModel` fail-closed（`account_upstream_capability.go:154-163`）vs `Annotate` 用的 `snapshotCoversRequestedModel`（`:165-187`）），且 R4 要重写 union/展开/coverage。R1 至少有“语义澄清 + 平台覆盖表 + R4 侧改动”三项工作。
- 建议修法：R1 改述为“同步机制不动；快照语义（fail-open 保留、平台覆盖、coverage 口径）澄清并冻结为契约”。

### F-14［建议］验收标准 §11 有三项不可执行

- “`grep` 可验证”未给精确模式： naive `grep Default.*Mapping` 仍会命中保留项（Bedrock 别名 `domain/constants.go:188`、region 前缀）、历史迁移 SQL、测试残留与前端删除残留。须给出验收 grep（含允许列表）。
- “迁移前后对外分组公开名集合一致”未给测量方法（建议：逐分组 `/v1/models` 快照 diff + 路由 dry-run 全绿）。
- “无 404/上游 400”未界定观测窗口与抽样（建议：发布后 N 天、按 §11 第一条逐分组真实调用抽检）。
- 另：`openai_models_list.go:151`（mapping 空 → 列表原文返回）与未绑渠道分组回落（基线第 5 步“平台默认名单 ∪ 目录 ID”，经 `admin_group.go:274-276` → `PlatformDefaultModelIDs`）在 allowlist 删除后是什么，PR-C 必须给出，否则验收第一条对未绑渠道分组无规范（拍板项 P5）。

### F-15［建议］§8 缺四步：迁移工具、双文档更新、门禁映射、回滚声明；B/C/D 发布粒度未定

- 缺步：(a) 迁移工具先行（见 F-06）；(b) `MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` 第 3/5 步与本文收紧点的同步修订（否则两文档矛盾——§9 只说“细化”却没立更新任务）；(c) 每 PR 的 SOP 门禁映射（SOP §7：PR-A 踩调度+计费、PR-C 踩 migrations/ent → 须审计结论链接进 PR body；L2 CI 全绿）；(d) 回滚声明（好消息：显式化后旧版读显式行为不变，回滚只需切镜像、无需数据回滚——应写下来作为迁移安全论据）。
- 发布粒度：B/C/D 同一次 release train 还是分开发布？若 C（删 allowlist）先于 B（R4 选择器），中间态“定价输入”行为无规范；SOP §5.5 要求每 PR 说明蓝绿共存。建议 A→迁移→B+C+D 同 train 单次发布，或逐 PR 补中间态说明（拍板项 P6）。
- 基线 commit 存疑：文档 §0 基线 `origin/main = 36e19f93c`，本 worktree HEAD = `c36e5e96a`（`chore(release): prepare v0.2.2-ts.1`）。实施前确认审计基线 SHA（若 worktree 已超前，行号以实施分支为准重核 F-11）。

---

## 代码交叉核对

### 一致（文档引用与实际相符，行号为实测值）

| 文档引用 | 实际 | 结论 |
|---|---|---|
| `account.go:639 resolveStoredModelMapping` | `:639` 函数定义，三段回退 `:640-708` | 一致 |
| Antigravity 默认 `:643`／Kiro `:646`／grok `:649` | 与实际同行 | 一致 |
| xai `:649,666,705` | 三处 `xai.DefaultModelMapping()` 同行 | 一致 |
| geminicli 引用 `:656,695` | `IsGeminiGoogleOne` 两分支同行 | 一致（定义在 `pkg/geminicli/models.go:36`，文档未给行，可补） |
| `ensureAntigravityDefaultPassthroughs` `:756`、`applyAntigravityGemini31ProAliases` `:762` | 同行 | 一致 |
| `IsModelSupported account.go:905` | `:905`，体 `:905-931` | 一致 |
| `ModelMappingRestricts account.go:884` | `:884`，`CredentialKey…` 在 `:856`，缺失默认 true 在 `:888-891` | 一致 |
| `applyCatalogDefaultMappings platform_default_models.go:90`，restricts 判断 `:91` | 同行；注入仅发生在 `!ModelMappingRestricts()`（§6 配套正确） | 一致 |
| xai `RuntimeModelMappingVersion :27`、`DefaultModelMapping :150` | `pkg/xai/models.go:27,150` 同行 | 一致 |
| `setting_parse.go:1006` | `xai.SetRuntimeModelMappingOptions` 同行（体 `:1006-1009`） | 一致 |
| `openai_messages_dispatch.go:75` 为 `xai.RuntimeModelMappingOptions()` 调用 | 同行 | 行号一致、**定性不符**（见 F-02：是分组调度改写，非账号注入） |
| `bedrock_request.go:129` 查 `DefaultBedrockModelMapping` | 同行（定义 `domain/constants.go:188`，region 前缀 `BedrockCrossRegionPrefix :27`、`Adjust… :53`、`bedrockCrossRegionPrefixes :23`） | 一致（文档 `:23-61` 范围大致覆盖，精确行如左） |
| `HasSyncedUpstreamModel :136`，fail-open `:141-143` | `account_upstream_capability.go` 同行 | 一致 |
| `AnnotateCatalogStorefrontCoverage :267`、`filterStorefrontCoverageAccounts :229` | `channel_storefront.go` 同行 | 一致 |
| 默认映射 endpoint `server/routes/admin.go:441-442` | 同行（`GET …/antigravity|kiro/default-model-mapping`） | 一致 |
| `isOpenAIOAuthServableModel` 保留 | `openai_model_mapping.go:68`（文档未给 file:line，建议补） | 存在，行号可补 |
| R3 主文件存在 | `domain/model_allowlist.go`、`service/group_model_allowlist.go`、`service/admin_group.go`（allowlist :571,1105）、`service/group.go:112`、`handler/gateway_handler.go`（:1185,1214,1359,1379,1643）、`service/openai_models_list.go`（:197,322）、`service/openai_codex_models_service.go`（:1312-1313）、`ent/schema/group.go:270-271`、迁移 `275` | 存在 |
| R2/前端主文件存在 | `useModelWhitelist.ts`（preset :298-441，`getPresetMappingsByPlatform :520`，`fetchAntigravity :449`，`fetchKiro :463`）、`EditAccountModal.vue`、`BulkEditAccountModal.vue`、`CreateAccountModal.vue`（如 `modelRestrictionMode :4857`）、`ChannelsView.vue` catalogPicker `:519-555`、coverage `:1131-1148`、`api/admin/channels.ts:224` | 存在 |
| `VERSION = 0.2.2-ts.1`、tag 存在 | 实测相符 | 一致 |

### 不符／漂移（需按 F-11 批量修正，另含遗漏）

| 文档引用 | 实际 | 结论 |
|---|---|---|
| `storefrontModelUnion :176` | `channel_storefront.go:165` | 差 11 行，修正 |
| `storefrontModelsFromUnion :196` | `:190` | 差 6 行，修正 |
| `ListCatalogStorefrontModelsWithCoverage :117` | `:122`（体 `:122-149`；union 语义见文件头注释 `:107-121`：现状 union = 快照原生名，非映射名——R4 确须重写，方向成立） | 差 5 行，修正 |
| `modelcatalog/catalog.go:411` | 包级 `DefaultMappings` 在 `:406`，`:411` 是 `(Catalog)` 方法版；调用方用包级版 | 改引 `:406`（可双列 406/411） |
| `openai_gateway_scheduling.go:483` | 函数在 `:481`（体 `:481-502`） | 差 2 行，修正 |
| antigravity 增补调用点 `:674-684` | 实际 `:679`（passthrough 8 名单）与 `:689`（31-pro 别名） | 范围漏 `:689`，修正 |
| `admin/account_handler.go:3399-3405` | 函数实际 `:3400-3408` | 差 1～3 行，修正 |
| `openai_gateway_handler.go:3732` | 定义实际 `:3731`；另有调用 `:2376,:2808` 未列 | 修正 + 补调用点 |
| `DefaultAntigravityModelMapping(:643,663)` 第二次引用 | 第二次 Antigravity 出现实际 `:660`，`:663` 是 Kiro 第二次出现（第三段 `:699/:702` 文档未编号，建议补） | 行号小错，修正 |
| PR-C 后端清单（10 文件） | 遗漏中间件/网关/DTO/缓存/ent-codegen（见 F-07） | 补全 |
| PR-C 前端清单 | 遗漏 `groupModelAllowlist.ts`、`modelAllowlistCandidates.ts` 及其单测（见 F-07） | 补全 |
| PR-A 清单 `openai_messages_dispatch.go:75` 定性为“应用点” | 实为分组调度改写（见 F-02） | 重新定性 + 补 `settings_view.go`、`domain_constants.go` 决策 |
| 文档“约 205 行” | 实际 192 行 | 顺手改 |
| 基线 `origin/main = 36e19f93c` | worktree HEAD `c36e5e96a` | 实施前确认基线 SHA（见 F-15） |

### 遗漏的默认／回落路径核查结论（已 grep 全仓）

- 账号级“空映射 → 默认映射”注入仅两处：`resolveStoredModelMapping`（平台默认）与 `applyCatalogDefaultMappings`（catalog 改写）——文档齐全，无第 6 层账号注入。
- 非映射类“默认名单”两处，不在零默认射程内但须正名去留：`PlatformDefaultModelIDs`（`platform_default_models.go:20-35`，经 `admin_group.go:274-276` 用于候选与 `/v1/models` 回落）去留未定；`supplementUnmappedOpenAIModels`（`group_model_allowlist.go:34-45`）随 R3 删除——建议文档点名。
- `channel.go:604-629` 的 channel mapping 空跳过、`batch_image_public.go:1275-1299` 的空映射返回 nil、`openai_models_list.go:151` 的空映射原文返回均为“空则无为”，非注入，无需删除。

---

## 需用户拍板项

- **P1（阻断）**：R4 选择器语义——∪（§3/§5/§11：keys + 快照原生名）还是 XOR（§4.3：有映射只展 keys）？推荐 ∪。
- **P2（阻断）**：R4 精确定义——通配展开源（快照枚举？空快照怎么办？跨客户端通配？）、coverage 分子分母（unsynced 按 fail-open 计入还是 fail-closed 不计？沿用 `CoverageHave/Total/Synced` 三字段？）、catalog 回退源（`PlatformDefaultModelIDs` vs `modelcatalog.PublicIDs/StorefrontItems`）与警告形态。
- **P3（重要）**：xai 运行时删除是否连带 Grok 分组 dispatch 改写（`openai_messages_dispatch.go:71-80`）与两设置项（`settings_view.go:219-220`）？选项：(a) 保留 dispatch（推荐，最小 blast radius）；(b) 同步删除并声明 Grok+Claude 路径行为变更。
- **P4（重要）**：历史迁移回填的僵尸默认显式值——视为显式免迁，还是一律按当前默认重算？（影响 §4.2 步骤 1 判定规则。）
- **P5（重要）**：未绑渠道分组 `/v1/models` 回落与 `PlatformDefaultModelIDs` 去留；composite 分组 picker 行为（`filterStorefrontCoverageAccounts` 要求 platform 非空，composite 需特判）。
- **P6（建议）**：B/C/D 单次 release train 发布，还是分开发布？若分开，补每 PR 蓝绿中间态说明（SOP §5.5）。

---

## 修订后实施路径（建议）

1. 修文档：F-01（统一 ∪/XOR）、F-02（拆分 dispatch）、F-03（重写 §4.1 分支表）、F-04（平台覆盖表）、F-05（R4 精确定义）、F-11（行号）、F-07/F-08（清单与测试表）、F-14（可执行验收）、F-15（补四步+P6）。
2. 交付迁移工具（F-06）并跑 §8 第 1 步只读排查，拿“依赖默认映射的账号 × 定价公开名”清单与规模。
3. 按 §8 顺序执行（PR-A → 数据迁移 → B/C/D 同 train → 蓝绿发布 → SOP + 产品基线双文档更新），PR-A/PR-C 附 SOP §7 审计结论链接。
4. 实施分支上重核 F-11 行号（基线 SHA 见 F-15 尾）。
