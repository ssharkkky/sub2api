# Design: catalog-runtime-file

## 数据流

```
本仓库（唯一权威，PR 评审 + CI 不变量）
├─ deploy/data/model_prices.json (+ .sha256)      LiteLLM 全量价表，运行时单一权威价源
└─ backend/internal/modelcatalog/data/catalog.json (+ .sha256)
                                                  列表/结构层 + 底稿价卡（兜底 + CI 锚点）
   │
   ├─ 构建时：go:embed → 内嵌目录（终极兜底，永不缺失）
   │          COPY → /app/data/catalog.json（镜像离线种子）
   │
   └─ 运行时（自动拉取，PR 合并后 ≤10min 生效）：
        10min ticker → fetch hash_url → 锚点 == 生效内容 hash？
          ├─ 是 → 短路（连正文都不下载）
          └─ 否 → 下载正文 → sha256 二次比对 → Load/parsePricingData 完整校验
                → 原子换入（Replace + lock 覆盖重放 / 价表 merge）
                → tmp+fsync+rename 落本地缓存（data_dir）
        失败 → 保留上一份有效版本 + 指数退避（首次 10s 逐次翻倍 cap 10min，哈希/正文层失败都计数）+ 去重告警
```

## 来源优先级（catalog，高 → 低）

1. `pricing.catalog_file` 显式文件（运维意图/air-gap；存在时远程不覆盖，分歧只告警）
2. `pricing.catalog_url` 远程（仓库 main 分支）
3. `./data/catalog.json` 自动发现（= 远程本地缓存/镜像种子）
4. 内嵌目录（`go:embed`）

价表同构：`remote_url`（默认本仓库）→ 本地缓存 → `fallback_file` → 硬编码回退。

## 关键决策

1. **整份替换，不做字段合并**：两个远程文件都是完整文档，语义清晰；字段级临时调整由 `pricing.override_file` 承担，职责不重叠。
2. **校验先于换入**：catalog 走 `Load`（版本、重复 id/alias、price+price_ref 互斥、price_ref 闭包）；价表走 `parsePricingData`。只有完整通过才换入。
3. **原子换入**：`atomic.Pointer[Catalog]`，`*Catalog` 构建后不可变；在途请求全旧或全新。价表 lock 覆盖重放**不原地改动**已共享条目（克隆替换 + map 引用原子切换）。
4. **哈希锚点是去重锚点不是完整性门控**：`.sha256` 文件与正文失配时仅告警（维护者可能只改了数据没改锚点），正文仍走完整校验；锚点匹配则短路省下载。
5. **单一 10min ticker 双目标**：一次 select 同时决策价表/目录同步（未启用目标 = nil channel，nil `*time.Ticker` 取 `.C` 会 panic，必须持 channel 变量），无竞态、生命周期单一；60s 内容哈希轮询仅保留给显式 `catalog_file`（热修复，免 mtime 粒度/TOCTOU 问题），自动发现文件不再轮询（它是缓存，由同步器写）。
6. **指数退避**：持续失败（GitHub 429/断网）时 10s→20s→…→10min，不再每 tick 敲 GitHub；成功即清零。
7. **BillingService 目录回退改惰性解析**：硬编码 map 构造后不变；目录条目按请求从当前生效目录解析（`lookupExactFallbackPricing` 第二层 + 思考档共享卡）。修复「构造时一次性快照 → 热换入后假兜底、需重启才生效」的缺口，且不引入跨服务钩子。
8. **两文件而非一文件**：价表（LiteLLM 格式，`parsePricingData` 生产解析路径）与目录（列表/结构/重写/共享卡，扁平价表表达不了）格式与消费者不同；合并要动生产解析路径，风险大于收益。CI 三不变量把两者绑死，防漂移。
9. **仓库默认、上游可选**：代码默认值指向本仓库 raw 路径（fork 数据是 fork 身份）；Wei-Shaw 文件降级为迁移/发现输入，显式覆盖仍可切回。
10. **schema 只增不改**：新数据配旧代码安全（未知字段忽略），回滚 = 回滚 PR 或清空 `catalog_url`。

## 审计记录（2026-09-03，opencode 独立审计 agent，结论「需修复后合入」→ 已全部修复）

- **P0**：`startUpdateScheduler` 用 nil `*time.Ticker` 取 `.C` 进 select（默认配置 localTicker=nil / air-gap 配置 remoteTicker=nil 均启动即 panic 杀进程）→ 改持 nil channel（`<-chan time.Time`）；独立小程序实证 + 三形态回归测试。
- **P1**：`downloadPricingData` 把不可信远程锚点原样存为 `localHash` 同步锚点，坏锚点（非 hex/过期）会让价表更新永久冻结（目录侧存正文哈希，无此问题）→ 统一为「实际加载正文的哈希」；坏锚点最多每轮冗余下载 + 告警，自愈。
- **P1**：`syncWithRemote` 哈希拉取失败 `return nil` 被调度器记为成功（退避永不触发）→ 返回错误计入退避，与目录侧对齐。
- **P2 已修**：显式文件轮询 mtime/size 指纹 → 内容哈希指纹（免 FAT 2s 粒度漏检与 read/stat TOCTOU），文件与生效一致时清陈旧 last_error；启动期目录同步 15s 总预算（`syncCatalogRemoteCtx`）。
- **P2/P3 记录不改**：`Initialize()` 无生产调用者（保留兼容入口）；replay 只 upsert 不删除（下架 lock 模型在下次价表同步重建后自然消失，≤1 个同步周期）；无 hash URL 时每轮全量下载（默认已配 hash URL，文档化）；全包 `-race` 的 `gin.SetMode` 旧测试并发问题与本功能无关（功能子集 -race 全绿）。
- **确认无误**：审计逐条核对了设计 1-7（双文件/四级优先级/远程同构/单 ticker/惰性回退/CI 不变量/配置默认）+ 全仓残留快照搜索（无）+ 价表切换风险（276 表对目录 83 名全覆盖，旧 198 键 0 缺失，38 处覆盖方向已列）。

## 兼容性 / 回滚

- 不改任何配置：默认值已指向本仓库（新行为）；显式空 `catalog_url` + 空 `remote_url` = 纯本地/内嵌模式（旧行为）。
- 回滚镜像：新配置键对旧版本无害（viper 忽略未知键）；数据文件只增不改，旧代码读到新价表条目也安全。
- 数据库无 schema 变化。
