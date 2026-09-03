# Change: catalog-runtime-file

- Schema: spec-driven
- Created: 2026-09-02
- Branch: `codex/feat-catalog-runtime-file`
- Type: 功能（一个业务能力及其测试/文档）

## Why

渠道模型定价底稿（`catalog.json`）与模型价表此前经 `go:embed` / 上游远程文件生效：改价或加模型要么重建镜像，要么依赖本仓库之外的运行时远程价格源（Wei-Shaw model-price-repo）。定价列表是 fork 的核心业务数据，应当**坐在本仓库、由应用自动拉取**：更新路径是「仓库 PR 合并 → 运行实例 ≤10min 自动换入」。

## What Changes

- `modelcatalog` 包：`sync.Once` 不可变单例改为 `atomic.Pointer[Catalog]`；新增 `Current()` / `Replace()` / `LoadFile()`；`Default()` 保持兼容（初始 = 空目录，数据不再 `go:embed`，二进制零模型/价格数据）；别名价卡引用只在 canonical 带卡时隐式共享（合并文档里非 lock 模型无卡）。所有既有调用方零改动。
- **数据层**：单一合并文档 `deploy/data/models.json`（+ `models.json.sha256`）：`models` 段（56 模型 / 83 id+alias / 3 上游重写 / 2 条 lock 卡）+ `prices` 段（276 条 LiteLLM 格式，运行时单一权威价源）。`tools/generate_model_prices.py` 从「上游基线 ∪ 当前 prices 段 ∪ `FORK_OVERRIDES` 显式决议表 + lock 卡」生成/校验（`--check` 供 CI 复核）。旧两文件（`model_prices.json`、包内 `catalog.json` + `embed.go`）删除。
- **配置**：`pricing.remote_url` / `pricing.hash_url` 默认指向本仓库合并文档 raw 路径（显式覆盖仍可用，可切回 Wei-Shaw 扁平价表）；`pricing.catalog_url` / `catalog_hash_url` 独立目录文档兼容路径（默认关闭）；`pricing.catalog_file`（显式文件，优先级最高）。
- **PricingService**：
  - `applyModelData()` 统一换入事务：形态识别（merged / 独立目录 / 扁平价表）→ 目录段 `modelcatalog.Load` + 价表段 `parsePricingData` 完整校验 → 同一临界区原子双换入（`Replace` + 价表 map 整体替换 / lock 覆盖重放）→ tmp+fsync+rename 原子落盘 + 锚点；
  - 启动期本地探测（`models.json` → 旧 `catalog.json` → 旧 `model_pricing.json`）+ 锚点即时比对（变化立即拉取，不等首轮 tick）；
  - 统一 10min ticker 调度远程目标（未启用目标 = nil channel）；60s 内容哈希轮询仅保留给显式 `catalog_file`（热修复；文件移除时解除本地赢锁）；
  - 拉取失败指数退避（首次 10s 逐次翻倍 cap 10min，哈希/正文层失败都计数）+ 整文档拒收保留 last-good + 去重告警；
  - 本地同步锚点 = 实际加载正文的哈希，坏锚点不冻结更新；
  - 优先级：显式文件 > 远程合并文档 > 本地缓存/种子探测 > 兜底价表 + 空目录；显式与远程分歧时本地赢 + 一次性告警 + `GetStatus` 暴露双 hash。
- **BillingService**：目录 baseline 回退从「构造时一次性快照进 map」改为**惰性解析**（命中当前生效目录）；lock 卡覆盖走克隆，不污染共享指针。
- `GetStatus()`：`catalog{source, path, models, loaded_at, hash, remote_hash, last_error}`（source ∈ remote / 显式路径 / released / none / 本地路径）+ 顶层 `remote_hash`。
- **CI 不变量**（`pricing_catalog_consistency_test.go`，全部走生产代码路径）：① `models.json.sha256` 与文档匹配；② 目录每个 id/alias 在 prices 段有显式条目；③ 每个 lock 模型带可解析卡且价表同名字段与卡相等；④ 目录段通过 `modelcatalog.Load` 生产校验器。
- 三个 Dockerfile + `.goreleaser.yaml`：构建时 COPY `deploy/data/models.json` 到 `/app/data/models.json`（离线种子；HTTP 服务前加载）。
- 文档：`docs/MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` §3/§9/§10、`deploy/config.example.yaml`、openspec。

## 关键不变式（用户验收要求）

**不更新二进制/版本，仓库更新文件即可适配新模型**：模型列表（`/v1/models`、渠道货架、平台默认）、别名/上游重写、lock 价、计费价全部由合并文档驱动（现有平台 + token 计费模态内成立）；仅需新 platform 枚举 / 新计费模态 / 家族计费策略常量时才改代码。

## Non-goals

- 不改定价优先级链（渠道价 > 分组价 > 目录锁定价 > 价表 > 兜底）。
- 不做管理后台目录编辑 UI（后续单独 PR，写库）。
- 不做「发现新官方模型」草稿 PR 自动化（计划文档第 5 步）。
- 不改 `VERSION`、不打 tag、不部署生产（SOP §3）。

## Acceptance

- 改价/加模型只改仓库 `deploy/data/models.json`（走 PR）：运行实例 ≤10min 自动拉取换入，无需重建镜像、无需重启、无需更新二进制/版本。
- 坏数据（非法版本/截断写入/远程不可达）不崩溃、不清空货架：整文档拒收，回落上一份有效版本（本地缓存/种子 → 兜底价表），状态接口可见错误，恢复后自动收敛。
- 目录与价表同事务原子换入，对计费回退、定价 resolver、渠道货架、`/v1/models`、默认映射全部透明，零调用点改动。
- CI 四不变量把合并文档绑成一组：任一段单独漂移测试变红。
- `go build ./...`、`internal/modelcatalog` 与 `internal/service` 全量测试、`go vet`、`python3 tools/generate_model_prices.py --check`、`deploy/tests/docker-runtime-resources-test.sh` 通过。
