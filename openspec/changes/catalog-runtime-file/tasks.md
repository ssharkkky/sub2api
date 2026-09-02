# Tasks: catalog-runtime-file

- [x] T1 `modelcatalog`：atomic.Pointer 激活目录 + `Current()`/`Replace()`/`LoadFile()`，`Default()` 兼容
- [x] T2 `config`：`pricing.catalog_file` 字段与默认值
- [x] T3 `service`：`catalogRuntime` 状态 + `loadRuntimeCatalogFile` / `startCatalogFileWatcher` / `pollCatalogFile`（校验先于换入、坏文件保留旧目录、告警去重）
- [x] T4 `wire.go`：`ProvidePricingService` 接入启动加载与 watcher 生命周期
- [x] T5 `GetStatus()`：`catalog` 来源/模型数/加载时间/最近错误
- [x] T6 镜像：三个 Dockerfile + `.goreleaser.yaml` 携带仓库目录文件到 `/app/data/catalog.json`
- [x] T7 配置样例与文档：`config.example.yaml`、`MODEL_CATALOG_AND_CHANNEL_STOREFRONT.md` §10
- [x] T8 测试：modelcatalog 换入/LoadFile；service 路径解析/启动加载/热加载/坏文件/文件消失/状态
- [x] T9 验证：`go build ./...`、两包全量测试、`deploy/tests/docker-runtime-resources-test.sh`、`go vet`
