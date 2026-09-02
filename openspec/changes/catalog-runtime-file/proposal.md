# Change: catalog-runtime-file

- Schema: spec-driven
- Created: 2026-09-02
- Branch: `codex/feat-catalog-runtime-file`
- Type: 功能（一个业务能力及其测试/文档）

## Why

渠道模型定价底稿（`catalog.json`）目前经 `go:embed` 编译进二进制，改价或加模型必须重建镜像、重新部署。定价列表是 fork 的核心业务数据，应当是**本仓库掌握的运行时数据**：更新路径是「仓库改文件 → 部署把文件放进容器 → 热加载生效」，而不是只能靠重建镜像，也不依赖任何本仓库之外的运行时远程价格源。

## What Changes

- `modelcatalog` 包：`sync.Once` 不可变单例改为 `atomic.Pointer[Catalog]`；新增 `Current()` / `Replace()` / `LoadFile()`；`Default()` 保持兼容（等价 `Current()`）。所有既有调用方零改动。
- 新增 `pricing.catalog_file` 配置（默认空）：显式路径优先，否则自动发现 `./data/catalog.json`、`./catalog.json`；缺失/非法回落内嵌目录。
- `PricingService` 新增目录文件生命周期：启动时加载（`loadRuntimeCatalogFile`）、60s mtime/size 轮询热加载（`startCatalogFileWatcher` / `pollCatalogFile`）；换入前完整校验（版本、重复 ID、price_ref 闭包），校验失败保留上一份有效目录并告警（相同错误只告警一次）。
- `GetStatus()` 新增 `catalog` 段：来源（文件路径或 `embedded`）、模型数、加载时间、最近错误。
- 三个 Dockerfile（`Dockerfile` / `deploy/Dockerfile` / `Dockerfile.goreleaser`）+ `.goreleaser.yaml` extra_files：构建时把仓库内 `catalog.json` COPY 到 `/app/data/catalog.json`（与内嵌基线同一份文件）。
- `deploy/config.example.yaml` 与 `docs/MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md`（新增 §10）记录配置与更新流程。

## Non-goals

- 不改定价优先级链（渠道价 > 分组价 > 目录锁定价 > override_file > LiteLLM/兜底）。
- 不引入运行时网络拉取价目（LiteLLM 远程镜像仍只当「发现新官方模型」的输入）。
- 不做管理后台目录编辑 UI（后续单独 PR，写库）。
- 不做「发现新官方模型」草稿 PR 自动化（计划文档第 5 步）。
- 不改 `VERSION`、不打 tag、不部署生产（SOP §3）。

## Acceptance

- 改价/加模型只改仓库 JSON：容器内文件更新后 ≤60s 热加载生效，无需重建镜像。
- 坏文件（非法版本/截断写入）不崩溃、不清空货架，回落上一份有效目录，状态接口可见错误，文件修复后自动恢复。
- 文件与内嵌目录都是本仓库同一份文件的两种到达路径；无第三方价格源进入运行时。
- `go build ./...`、`internal/modelcatalog` 与 `internal/service` 全量测试、`deploy/tests/docker-runtime-resources-test.sh` 通过。
