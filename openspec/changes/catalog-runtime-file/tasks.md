# Tasks: catalog-runtime-file

- [x] T1 `modelcatalog`：atomic.Pointer 激活目录 + `Current()`/`Replace()`/`LoadFile()`，`Default()` 兼容
- [x] T2 `config`：`pricing.catalog_file` 字段与默认值
- [x] T3 `service`：`catalogRuntime` 状态 + `loadRuntimeCatalogFile` / `pollCatalogFile`（校验先于换入、坏文件保留旧目录、告警去重）
- [x] T4 `wire.go`：`ProvidePricingService` 接入启动加载与调度生命周期
- [x] T5 `GetStatus()`：`catalog` 来源/模型数/加载时间/最近错误
- [x] T6 镜像：三个 Dockerfile + `.goreleaser.yaml` 携带仓库目录文件到 `/app/data/catalog.json`
- [x] T7 配置样例与文档：`config.example.yaml`、`MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md`
- [x] T8 测试：modelcatalog 换入/LoadFile；service 路径解析/启动加载/热加载/坏文件/状态
- [x] T9 数据迁移：`tools/generate_model_prices.py`（上游 229 基线 + 83 目录名全覆盖 + 47 合成 + 38 处卡价覆盖报告）→ `deploy/data/model_prices.json` (276) + 双 `.sha256` 锚点；`--check` 供 CI 复核
- [x] T10 `config`：`pricing.catalog_url`/`catalog_hash_url`；`remote_url`/`hash_url` 默认切到本仓库
- [x] T11 `service`：`syncCatalogRemote()`（锚点短路 → 下载 → 校验 → `swapInCatalog`：Replace + lock 重放 + 原子落缓存）+ `fetchCatalogRemoteHash` + 四级优先级（显式文件赢 + 分歧告警）
- [x] T12 `service`：统一 10min ticker 双远程目标；60s 轮询仅显式文件；`remoteBackoff` 指数退避（10s×2ⁿ cap 10min）；`writeAtomic`（tmp+fsync+rename）用于价表/目录缓存落盘
- [x] T13 `billing`：目录 baseline 回退改惰性解析（热换入即时可见，修假兜底）；lock 卡覆盖走克隆；`ListSupportedModels`/`HasIdentifiedTokenPricing`/status 同步口径
- [x] T14 `GetStatus()`：`catalog{hash, remote_hash, remote_enabled}` + 顶层价表 `remote_hash`
- [x] T15 CI 不变量：`pricing_catalog_consistency_test.go`（sha256 匹配 / 目录名全覆盖 / 卡价相等）
- [x] T16 测试：远程同步 9 例（锚点短路/换入/坏体保留/下载失败/显式赢/无锚点回退/hash 失败/退避/URL 正确）+ lock 重放克隆 + billing 惰性热换入 4 例 + 一致性 3 例
- [x] T17 验证：`go build ./...`、`internal/service` + `internal/modelcatalog` + `internal/config` 全量测试、`go vet`、`python3 tools/generate_model_prices.py --check`
