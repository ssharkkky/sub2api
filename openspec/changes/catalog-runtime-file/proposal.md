# Change: catalog-runtime-file

- Schema: spec-driven
- Created: 2026-09-02
- Branch: `codex/feat-catalog-runtime-file`
- Type: 功能（一个业务能力及其测试/文档）

## Why

渠道模型定价底稿（`catalog.json`）与模型价表此前经 `go:embed` / 上游远程文件生效：改价或加模型要么重建镜像，要么依赖本仓库之外的运行时远程价格源（Wei-Shaw model-price-repo）。定价列表是 fork 的核心业务数据，应当**坐在本仓库、由应用自动拉取**：更新路径是「仓库 PR 合并 → 运行实例 ≤10min 自动换入」，编译进二进制的只作终极兜底。

## What Changes

- `modelcatalog` 包：`sync.Once` 不可变单例改为 `atomic.Pointer[Catalog]`；新增 `Current()` / `Replace()` / `LoadFile()`；`Default()` 保持兼容。所有既有调用方零改动。
- **数据层**：新增 `deploy/data/model_prices.json`（+ `.sha256`，276 条 LiteLLM 格式，`tools/generate_model_prices.py` 从上游发现基线 + 目录价卡生成/校验，CI `--check` 可复核）；`catalog.json` 旁新增 `catalog.sha256` 锚点。
- **配置**：`pricing.catalog_url` / `pricing.catalog_hash_url`（默认本仓库 main 分支 raw 路径）；`pricing.remote_url` / `pricing.hash_url` 默认从 Wei-Shaw 切到本仓库（显式覆盖仍可用）；`pricing.catalog_file`（显式文件，优先级最高）。
- **PricingService**：
  - `syncCatalogRemote()`：哈希锚点比对 → 变化才下载 → `modelcatalog.Load` 完整校验 → `swapInCatalog`（`Replace` + 价表 lock 覆盖重放 + tmp/fsync/rename 原子落缓存，同一事务内）；
  - 统一 10min ticker 同时调度价表/目录两个远程目标；60s mtime/size 轮询仅保留给显式 `catalog_file`；
  - 拉取失败指数退避（10s×2ⁿ cap 10min）+ 保留上一份有效版本 + 去重告警；
  - 四级优先级：显式文件 > 远程 > 自动发现缓存 > 内嵌；显式与远程分歧时本地赢 + 一次性告警 + `GetStatus` 暴露双 hash。
- **BillingService**：目录 baseline 回退从「构造时一次性快照进 map」改为**惰性解析**（`lookupExactFallbackPricing` 命中当前生效目录）——目录热换入后计费回退立即可见，修复「假兜底」（需重启才生效）缺口；lock 卡对硬编码条目的覆盖走克隆，不污染共享指针。
- `GetStatus()`：`catalog{source, path, models, loaded_at, hash, remote_hash, last_error}` + 顶层 `remote_hash`。
- **CI 不变量**（`pricing_catalog_consistency_test.go`）：① 双 `.sha256` 与数据文件匹配；② 目录每个 id/alias 在价表有显式条目；③ 目录价卡与价表同名字段相等。
- 三个 Dockerfile + `.goreleaser.yaml`：构建时 COPY `catalog.json` 到 `/app/data/catalog.json`（离线种子基线）。
- 文档：`docs/MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` §9/§10、`deploy/config.example.yaml`。

## Non-goals

- 不改定价优先级链（渠道价 > 分组价 > 目录锁定价 > 价表 > 兜底）。
- 不做管理后台目录编辑 UI（后续单独 PR，写库）。
- 不做「发现新官方模型」草稿 PR 自动化（计划文档第 5 步）。
- 不改 `VERSION`、不打 tag、不部署生产（SOP §3）。

## Acceptance

- 改价/加模型只改仓库 JSON（走 PR）：运行实例 ≤10min 自动拉取换入，无需重建镜像、无需重启。
- 坏数据（非法版本/截断写入/远程不可达）不崩溃、不清空货架：回落上一份有效版本（本地缓存 → 内嵌），状态接口可见错误，恢复后自动收敛。
- 目录热换入对计费回退、定价 resolver、渠道货架、`/v1/models`、默认映射全部透明，零调用点改动。
- CI 三不变量把两文件绑成一组：任一侧单独漂移测试变红。
- `go build ./...`、`internal/modelcatalog` 与 `internal/service` 全量测试、`go vet`、`deploy/tests/docker-runtime-resources-test.sh` 通过。
