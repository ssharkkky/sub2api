# Design: catalog-runtime-file

## 数据流

```
本仓库（唯一权威，PR 评审 + CI 不变量）
└─ deploy/data/models.json (+ .sha256)      单一合并文档：
                                            models 段（目录：id/alias/重写/lock 卡）
                                            + prices 段（LiteLLM 全量价表，运行时单一权威价源）
   │
   ├─ 构建时：COPY → /app/data/models.json（镜像离线种子；二进制零模型/价格数据，无 go:embed）
   │
   └─ 运行时（自动拉取，PR 合并后 ≤10min 生效）：
        10min ticker → fetch hash_url → 锚点 == 本地已加载正文 hash？
          ├─ 是 → 短路（连正文都不下载）
          └─ 否 → 下载正文 → sha256 二次比对 → 形态识别（merged/目录/扁平价表）
                → 目录段 modelcatalog.Load + 价表段 parsePricingData 完整校验
                → 同一临界区原子双换入（Replace + 价表 map 整体替换）
                → tmp+fsync+rename 落本地缓存 + 锚点（data_dir/models.json）
        失败 → 整文档拒收，保留上一份有效版本 + 指数退避（首次 10s 逐次翻倍 cap 10min，哈希/正文层失败都计数）+ 去重告警
```

## 来源优先级（高 → 低）

1. `pricing.catalog_file` 显式文件（运维意图/air-gap；目录段加载成功后赢过所有远程目录段，
   判定 = 「目录确实来自该文件」——文件损坏/未放入不锁死远程；60s 内容哈希轮询热修复；
   文件被移除时解除本地赢锁，远程重新接管）
2. `pricing.remote_url` 合并文档（默认 = 仓库 main 分支 `deploy/data/models.json`；目录+价表一次到位）
3. 启动期本地探测：`./data/models.json`（远程缓存/镜像种子）→ `./data/catalog.json`（旧独立目录缓存）
   → `./data/model_pricing.json`（旧价表缓存；后两者只为两文件时代部署平滑迁移而保留）
4. 兜底价表（`fallback_file`，镜像内置 198 条）+ 空目录——价目表永不为空（计费 fail-closed），
   目录空只影响货架展示，远程恢复后自动收敛

`pricing.catalog_url` / `catalog_hash_url` 独立目录文档 = 兼容路径（默认关闭），
与合并文档同一套「锚点 → 变化才下载 → 校验 → 原子换入」节奏。

## 关键决策

1. **整份替换，不做字段合并**：两个远程文件都是完整文档，语义清晰；字段级临时调整由 `pricing.override_file` 承担，职责不重叠。
2. **校验先于换入**：catalog 走 `Load`（版本、重复 id/alias、price+price_ref 互斥、price_ref 闭包）；价表走 `parsePricingData`。只有完整通过才换入。
3. **原子换入**：`atomic.Pointer[Catalog]`，`*Catalog` 构建后不可变；在途请求全旧或全新。价表 lock 覆盖重放**不原地改动**已共享条目（克隆替换 + map 引用原子切换）。
4. **哈希锚点是去重锚点不是完整性门控**：`.sha256` 文件与正文失配时仅告警（维护者可能只改了数据没改锚点），正文仍走完整校验；锚点匹配则短路省下载。
5. **单一 10min ticker 双目标**：一次 select 同时决策价表/目录同步（未启用目标 = nil channel，nil `*time.Ticker` 取 `.C` 会 panic，必须持 channel 变量），无竞态、生命周期单一；60s 内容哈希轮询仅保留给显式 `catalog_file`（热修复，免 mtime 粒度/TOCTOU 问题），自动发现文件不再轮询（它是缓存，由同步器写）。
6. **指数退避**：持续失败（GitHub 429/断网）时 10s→20s→…→10min，不再每 tick 敲 GitHub；成功即清零。
7. **BillingService 目录回退改惰性解析**：硬编码 map 构造后不变；目录条目按请求从当前生效目录解析（`lookupExactFallbackPricing` 第二层 + 思考档共享卡）。修复「构造时一次性快照 → 热换入后假兜底、需重启才生效」的缺口，且不引入跨服务钩子。
8. **单一合并文档（models + prices）**：目录与价表同文档、同校验、同事务原子换入，从机制上消除两文件时代的「反向 skew 窗口」（新目录 + 旧价表）。形态识别兼容三种文档（merged / 独立目录 / 扁平价表），旧两文件部署的本地缓存可被探测加载并随首次远程同步收敛。`parsePricingData` 生产解析路径不变（prices 段就是 LiteLLM map）。
9. **删除 go:embed**：二进制零模型/价格数据。所有目录消费者 nil-safe（空目录 = 货架暂空，计费走价表/兜底）；官方镜像种子 `/app/data/models.json` 在 HTTP 服务前加载，消除启动空目录窗口；裸二进制无种子时目录从首次远程同步起生效（价表有 198 条兜底）。
10. **仓库默认、上游可选**：代码默认值指向本仓库 raw 路径（fork 数据是 fork 身份）；Wei-Shaw 文件降级为迁移/发现输入（生成器的上游基线），显式覆盖仍可切回。
11. **schema 只增不改**：新数据配旧代码安全（未知字段忽略），回滚 = 回滚 PR 或清空 `remote_url`。
12. **fork 决议值显式化**：非 lock 模型不带价卡（价格全部由 prices 段承载）；fork 侧高于上游的决议值集中在生成器 `FORK_OVERRIDES` 表，机器校验、漂移必被抓；lock 卡（2 条）留在 models 段，CI 不变量强制价表字段与卡相等。

## 审计记录

### 第一轮（2026-09-03，opencode 独立审计 agent，双文件版本，结论「需修复后合入」→ 已全部修复）

- **P0**：`startUpdateScheduler` 用 nil `*time.Ticker` 取 `.C` 进 select（默认配置 localTicker=nil / air-gap 配置 remoteTicker=nil 均启动即 panic 杀进程）→ 改持 nil channel（`<-chan time.Time`）；独立小程序实证 + 三形态回归测试。
- **P1**：`downloadPricingData` 把不可信远程锚点原样存为 `localHash` 同步锚点，坏锚点（非 hex/过期）会让价表更新永久冻结（目录侧存正文哈希，无此问题）→ 统一为「实际加载正文的哈希」；坏锚点最多每轮冗余下载 + 告警，自愈。
- **P1**：`syncWithRemote` 哈希拉取失败 `return nil` 被调度器记为成功（退避永不触发）→ 返回错误计入退避，与目录侧对齐。
- **P2 已修**：显式文件轮询 mtime/size 指纹 → 内容哈希指纹（免 FAT 2s 粒度漏检与 read/stat TOCTOU），文件与生效一致时清陈旧 last_error；启动期目录同步 15s 总预算（`syncCatalogRemoteCtx`）。
- **P2/P3 记录不改**：`Initialize()` 无生产调用者（保留兼容入口）；replay 只 upsert 不删除（下架 lock 模型在下次价表同步重建后自然消失，≤1 个同步周期）；无 hash URL 时每轮全量下载（默认已配 hash URL，文档化）；全包 `-race` 的 `gin.SetMode` 旧测试并发问题与本功能无关（功能子集 -race 全绿）。
- **确认无误**：审计逐条核对了设计 1-7（双文件/四级优先级/远程同构/单 ticker/惰性回退/CI 不变量/配置默认）+ 全仓残留快照搜索（无）+ 价表切换风险（276 表对目录 83 名全覆盖，旧 198 键 0 缺失，38 处覆盖方向已列）。

### 第二轮（2026-09-03，opencode 独立审计 agent，对象 = 含合并单文档的 5 提交，结论「修复后可合并」→ 已全部修复）

- **无 P0**：三锁（s.mu / catalogRuntime.mu / atomic.Pointer）逐行核对无嵌套重入、无反转；读者路径全旧或全新且 nil-safe；`-race` 功能子集 116 断言 0 FAIL；276 条价表 vs 旧表逐键逐字段 0 差异；生成器幂等（committed == 生成输出）；前轮 6 项修复无回归。
- **P1 已修**：boot 窗口本地赢失效——显式 `catalog_file` 先加载、随后 `InitializeCtx` 本地探测以 `path=种子路径(≠"")` 换入绕过让位判定，每次重启 ≤60s 内货架/别名/重写/lock 价按种子而非运维意图生效 → wire.go 顺序对调（InitializeCtx → 独立 catalog_url 同步 → 显式文件最后应用）；回归测试走真实 `ProvidePricingService` 路径。
- **P2 已修**：畸形合并文档 `models 数组 + prices 非对象` 被静默降级为 catalog-only（prices 段无声丢弃，违背整份拒收契约）→ `classifyModelData` 收紧：section 键存在但形态不符 → `shapeUnknown` 整份拒收；11 例形态契约测试固化。
- **P3 已修**：兜底拷贝改 `writeAtomic`（崩溃不留半截缓存）；docker 资源测试补 4 处 `models.json` 种子断言 + 种子文件存在性检查。

## 兼容性 / 回滚

- 不改任何配置：默认值已指向本仓库（新行为）；显式空 `catalog_url` + 空 `remote_url` = 纯本地/内嵌模式（旧行为）。
- 回滚镜像：新配置键对旧版本无害（viper 忽略未知键）；数据文件只增不改，旧代码读到新价表条目也安全。
- 数据库无 schema 变化。
