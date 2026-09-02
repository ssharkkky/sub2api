# Design: catalog-runtime-file

## 数据流

```
本仓库 catalog.json（唯一权威，PR 评审 + CI）
   ├─ 构建时：go:embed → 二进制内嵌基线（兜底，永不缺失）
   │          COPY → /app/data/catalog.json（镜像内文件，与内嵌同内容）
   └─ 运行时：部署把仓库文件放进 /app/data/catalog.json（或 pricing.catalog_file 指定路径）
              → PricingService 启动加载 / 60s 轮询
              → LoadFile() 完整校验 → modelcatalog.Replace() 原子换入
              → 所有调用方（resolver/货架//v1/models/默认映射）透明生效
```

## 关键决策

1. **整份替换，不做字段合并**：运行时文件与仓库文件是同一个 schema（`version:1` 文档），语义清晰；字段级临时调整已有 `pricing.override_file` 承担，职责不重叠。
2. **校验先于换入**：`LoadFile` 复用 `Load` 的全部校验（版本、重复 id/alias、price+price_ref 互斥、price_ref 闭包）。只有完整通过才 `Replace`；半坏文件不可能成为生效状态。
3. **原子换入**：`atomic.Pointer[Catalog]`，`*Catalog` 构建后不可变（map 只读）。在途请求持有旧 `*Entry` 指针也安全（旧目录对象仍被引用，不会半更新）。
4. **坏文件降级而非失败**：启动期文件非法 → 内嵌基线继续生效 + 告警；运行期文件变坏 → 保留上一份有效目录 + 告警（相同错误只告警一次，防刷屏）；文件消失（部署中间态）→ 下轮重试。
5. **mtime+size 轮询而非 inotify**：文件仅几 KB，stat 开销可忽略；无 fsnotify 依赖；NFS/容器卷上行为一致。轮询间隔 60s 为常量，足够「仓库更新即生效」的运维节奏。
6. **watcher 生命周期挂 PricingService**：与 `pricing_update` 调度器同生命周期（`ProvidePricingService` 启动、`Stop()` 随 `stopCh` 退出、`wg` 汇合），不新增 wire 节点、不新增进程级后台任务框架。
7. **内嵌基线保留**：即使部署忘记挂载文件，二进制仍有完整价格底稿（与仓库构建时同一份），行为与升级前一致，天然回滚安全。

## 兼容性 / 回滚

- 不升级镜像、不挂文件：行为与升级前完全一致（内嵌目录）。
- 回滚镜像：`pricing.catalog_file` 配置残留无害（旧版本忽略未知键）。
- 数据库无 schema 变化。
