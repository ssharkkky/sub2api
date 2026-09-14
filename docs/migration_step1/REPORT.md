# Step 1 生产只读排查报告（零风险，只读）

> **状态：✅ 完成。30/30 定价公开名全覆盖，数据预迁移=空操作，可进 PR-A。**

- **日期**：2026-09-08
- **生产**：`sub2api-green` v0.2.2-ts.1；DB `sub2api-postgres`
- **方法**：只读 `SELECT`（`ssh -p 22222 root@198.44.63.169` → `docker exec sub2api-postgres psql`），不改数据、不重启。
- **工具**：`verify_coverage.sql`（可重跑，stdin 注入）+ 本文。

## 1. 规模

| 维度 | 数值 |
|---|---|
| 活跃渠道 | 8 |
| **restrict 渠道**（`restrict_models=true` 且 active） | **7** |
| 活跃分组 | 9 |
| 总账号 | 204 |
| **定价行**（channel_model_pricing） | 29 |
| **restrict 渠道定价公开名（去重）** | **30** |
| restrict 渠道绑定账号（去重） | 25（29 含跨渠道重复） |
| **全量空 `model_mapping` 账号**（删注入后变透传） | **106 / 204**（openai 79、anthropic 23、grok 3、gemini 1、antigravity 0） |

## 2. 7 个 restrict 渠道的定价公开名（30 个）

| 渠道 | 平台 | 定价公开名 |
|---|---|---|
| 1 GPT Pro | openai | gpt-5.5, gpt-5.6-terra, codex-auto-review, gpt-5.4, gpt-6-astra, gpt-5.6-sol |
| 2 GPT Image | openai | gpt-image-2 |
| 4 Antigravity Gemini | antigravity | gemini-3.7-flash-{tiered,high,medium,low}, gemini-3.8-flash-{high,low,medium}, gemini-3.6-flash-{tiered,high,low,medium}, gemini-3.1-pro-{high,low}（13 个） |
| 5 Kiro | anthropic | claude-opus-5, claude-sonnet-5 |
| 7 Grok | grok | grok-4.5, grok-4.6 |
| 8 Antigravity Claude | antigravity | claude-sonnet-4-6, claude-opus-4-6-thinking |
| 9 Free models | openai | MiniMaxAI/MiniMax-M2.7, muse-spark-1.2-contributor-free, muse-spark-1.3-contributor-free, MiniMaxAI/MiniMax-M3 |

## 3. 覆盖交叉引用（删默认注入 before/after diff）

对每个定价公开名，检查是否被该渠道**绑定账号的显式 `model_mapping`（存储 keys）**覆盖（`verify_coverage.sql` 权威输出）：

- **30 / 30 显式覆盖（explicit_covered=t）**：删注入后仍由显式映射服务，**before = after，无变化、安全**。
  - 每个名字都有 ≥1 个绑定账号的显式映射命中（多数 2–9 个账号冗余覆盖）。
  - gpt-6-astra（原 404 模型）现由 3 个账号显式映射覆盖。
  - **muse-spark 两名已解决**（见第 4 节）：账号 88（opencode）已绑入分组 17，现由 1 个账号显式覆盖。

## 4. 2 个 muse-spark 名（已解决）

**现象**：Ch 9 Free models 定价表列了 `muse-spark-1.2-contributor-free` / `muse-spark-1.3-contributor-free`，但原绑定的 196/198 接不住（显式映射/快照/catalog/默认注入四层都不含），属**既有死条目**（列表可见、请求 404），与重构无关。

**定位**：账号 **88（opencode，openai，active）** 的显式映射恰有这两名（恒等映射），且其上游原生快照含这两个精确名——它在分组 11（Opencode Test，渠道 6 非 restrict）里本来就能正常服务这两名。

**处置（已执行）**：账号 88 已绑入分组 17（Free models）→ 重跑 `verify_coverage.sql` 确认两名 `explicit_covered=t`（1 账号）。**30/30 全覆盖，无残留死条目。**

> 注：账号 88 现同时服务分组 11 与 17，共享上游配额/速率（运维已确认接受）。

## 5. 空映射账号（106 个）迁移影响

- 这 106 个空映射账号删注入后由「平台默认映射」变「透传（原生快照）」。
- **生产 404 风险只落在 7 个 restrict 渠道**（定价门禁）；已验证 30 个定价名全部安全（28 显式 + 2 既有问题）。
- 绑定到 restrict 渠道的**唯一空映射账号是 186（grok）**；其渠道（Ch 7 Grok）的 grok-4.5/4.6 已由 183/191 显式覆盖 → 186 变透传**不影响** Ch 7。
- 其余空映射账号在**非 restrict 渠道**：变透传是**预期新行为**（R4 列表回落 + 透传路由），不产生用户可见 404。

## 6. 迁移结论（Step 1）

> **为防止 restrict 渠道 404，无需对生产数据做任何写入。** 全部 30 个定价公开名都已被绑定账号的显式 `model_mapping` 覆盖，删默认注入后 before=after。

- **muse-spark 两名已解决**：账号 88 绑入分组 17（运维已执行），30/30 全覆盖。
- **数据预迁移（§8 步骤 2）对本生产是空操作**：无账号需要补显式映射来保 404。
- **回滚安全**：除运维主动加的「账号 88→分组 17」绑定外无重构相关数据写入 → 无数据回滚需求；代码回滚即恢复现状。

## 7. 交付物

- `verify_coverage.sql` — 可重跑只读覆盖验证（发布前/后各跑一次，确认 before=after）。
- `bound_accounts.tsv` — 29 个绑定账号 × 平台 × 显式映射明细（原始数据）。
- 本报告 `REPORT.md`。

## 8. 下一步

- [x] muse-spark 2 名：账号 88 绑入分组 17，30/30 全覆盖（已解决）。
- [x] 数据预迁移=空操作 → 跳过 §8 步骤 2 的写入，直接进入 PR-A。
- [ ] **PR-A**（零默认映射 + 路由回退 + `mapAntigravityModel`）：本地全链路 + SOP 审计合并。
- [ ] 蓝绿发布前后各跑一次 `verify_coverage.sql`，确认 30 名 before=after。
