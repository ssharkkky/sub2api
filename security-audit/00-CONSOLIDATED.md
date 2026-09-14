# Sub2API 安全漏洞审计 — 最终合并报告

- 日期: 2026-09-02 · 审计范围: backend (Go/Gin/ent, ~42 万行) + frontend (Vue3)
- 方法: 3 个 Opencode subagent 分域扫描（AuthN/AuthZ · Injection/SSRF · 配额/计费/前端）+ orchestrator 对全部 CRITICAL/HIGH 逐条定向验证
- 分域明细: `01-authn-authz.md` · `02-injection-ssrf.md` · `03-gateway-quota-frontend.md`
- 结论: **无 CRITICAL 级认证绕过/提权/SQL 注入**；确认 1 个 CRITICAL SSRF、6 个 HIGH、9 个 MEDIUM、7 个 LOW、4 个 INFO。代码库整体硬化程度高（参数化 SQL、原子计费、HMAC webhook、DOMPurify、crypto/rand 等均已到位），漏洞集中在少数细节路径。

---

## CRITICAL

### C-1 · Kiro 图片 URL 服务端抓取 → SSRF（已验证）
- **位置**: `backend/internal/pkg/kiro/image_tokens.go:50-75,100`（`EstimateImageTokens` → `fetchRemoteImageTokens`），client 定义 `pkg/kiro/translator.go:69`
- **CWE-918** · 验证: 请求体中 Kiro 图片块 `source` 可为任意 HTTP(S) URL，服务端用裸 `http.Client{Timeout:8s}`（跟随重定向）抓取，**无私网 IP / 链路本地 / DNS-rebinding 防护**
- **攻击**: 任何持有 Kiro 分组 API key 的用户，在消息中嵌入 `image_url=http://169.254.169.254/latest/meta-data/iam/security-credentials/`（或内网服务地址）→ 盲 SSRF：可达性探测（成功/失败/延迟/尺寸信号）、内网端口扫描、云元数据泄露（间接）
- **修复**:
```go
// pkg/httpclient 新增 SafeDialContext：先解析 DNS，拒绝 私网/loopback/link-local/CGNAT
// (10/8,172.16/12,192.168/16,127/8,::1,fc00::/7,169.254/16,100.64/10)；
// 自定义 DialContext 使重定向后的每个 hop 都过同一检查（防止 DNS rebinding / 302 跳转绕过）。
kiroRemoteImageHTTPClient = &http.Client{Timeout: 8 * time.Second,
    Transport: &http.Transport{DialContext: safeDialer.DialContext, ...}}
```
  并在 `fetchRemoteImageTokens` 前对 `rawURL` 做 scheme 白名单（仅 http/https）+ host 预检；失败走 `kiroImageTokenFallback`（保持现有行为）。

---

## HIGH

### H-1 · 计费透支：无负余额下限（已验证）
- **位置**: `repository/usage_billing_repo.go:243-271`（`deductUsageBillingBalance` 第二条 UPDATE 无 `balance >= $1` 守卫）、预检 `service/billing_cache_service.go:871`
- **CWE-368** · 验证: 预检在 balance≤0 拒绝，但**在途并发请求**全部先过预检再异步扣款 → 用户可并发打出 `并发数 × 单请求成本` 的免费用量，余额无下限地转负，且无回收/告警
- **修复**:
  1. 扣款 SQL 加透支下限：`SET balance = GREATEST(balance - $1, -COALESCE($2, 0))`，$2 为用户透支上限（新用户默认 0，可配置）；
  2. 或在 `users` 表加 `overdraft` 列，预检改为 `balance - overdraft > 0`；
  3. 余额首次转负时触发 `balance_notify_service` 告警（复用现有通知链路）。

### H-2 · API Key IP 白/黑名单可被伪造 X-Forwarded-For 绕过（已验证）
- **位置**: `config/config.go:2079`（`security.trust_forwarded_ip_for_api_key_acl` **默认 true**）→ `middleware/api_key_auth.go` IP ACL → `pkg/ip/ip.go:147-236`（legacy 取 **最左公共 IP**）
- **CWE-346/350** · 验证: 应用直连公网或经默认配置 nginx（`$proxy_add_x_forwarded_for` 追加在右，最左仍为攻击者控制）时，持有 key 者发送 `X-Forwarded-For: <白名单IP>` 即可绕过该 key 的 IP 限制 → 被盗 key 可脱离 IP 管控
- **修复**:
  1. 默认值改 `false`（安全敏感路径一律走 Gin `server.trusted_proxies` 链，即 `GetTrustedClientIP`）；
  2. 需要 legacy 行为的部署显式开启并同时配置 `server.trusted_proxies`（文档 + 启动 warning 已有，补默认值）；
  3. `resolveLegacyForwardedHeaderIP` 的"最左公共 IP"选择改为"从右向左第一个非受信代理且非私网 IP"（与代理链语义一致）。

### H-3 · "登出所有设备"后 access JWT 仍有效最长 24h（已验证）
- **位置**: `service/auth_service.go:1894-1921`（`RevokeAllUserTokens` 仅删 refresh 缓存）；`TokenVersion` 由 email+password_hash 指纹推导（无独立列）；`config.go:2276` access token 默认 24h
- **CWE-613** · 验证: 改密能失效旧 token（指纹变化），但**主动登出所有设备 / 封禁前的已签发 access token** 在自然过期前（默认 24h）持续可用；`jwt_auth` 中间件每次请求查库但不比对撤销水位
- **修复**（二选一，推荐 1）:
  1. `users` 表加 `token_version bigint`（迁移默认 0）；`RevokeAllUserTokens` 执行 `UPDATE users SET token_version = token_version + 1`；`jwt_auth` 中现有 `claims.TokenVersion != user.TokenVersion` 检查改为与该列比对（claims 签发时写入该列值）——**复用现有中间件检查点，改动最小**；
  2. 或 Redis 存每用户 `revoked_at` 时间戳（TTL=access token 寿命），中间件拒绝 `iat < revoked_at` 的 token（Redis 故障时降级为现有行为并告警）。

### H-4 · TOTP 码窗口内可重放（无一次性消费）（已验证）
- **位置**: `service/totp_service.go:383`（`totp.Validate` 后无 used-code 缓存）
- **CWE-294/384** · 验证: 同一 TOTP 码在整个时间窗（30s × skew）内可多次通过 —— 截获/泄露一次 2FA 码可在窗口内重放用于多次登录尝试、重复获取 step-up 授权
- **修复**:
```go
// VerifyCode 成功后：
usedKey := fmt.Sprintf("totp_used:%d:%s", userID, sha256hex([]byte(code)))
if ok, _ := s.cache.Redis().SetNX(ctx, usedKey, 1, 90*time.Second).Result(); !ok {
    return ErrTotpReplay // 或静默拒绝
}
```
  TTL 覆盖窗口+skew（3×30s 足够）。注意与登录限流（`auth-login-2fa` 20/min）叠加使用。

### H-5 · 首页 homeContent v-html 未消毒 → 存储型 XSS（admin→全体用户）（已验证）
- **位置**: `frontend/src/views/HomeView.vue:12`（`v-html="homeContent"`，全文无 DOMPurify；同文件仅对 logo/doc_url 用 `sanitizeUrl`）
- **CWE-79** · 验证: `home_content` 是 admin 全局设置（`admin/setting_handler.go:271`）；admin 面板被入侵/低权 admin 即可向所有用户页面注入脚本；配合 H-6（token 存 localStorage）可窃取会话
- **修复**: 与 `CustomPageView.vue:244` 对齐 —— `const homeContent = computed(() => DOMPurify.sanitize(raw, {ADD_ATTR:['target','class']}))`（按现有 CSP 收敛标签白名单）；并在 admin 设置保存侧做一次同样的服务端预检（防其他消费方）。

### H-6 · 前端 JWT + refresh_token 存 localStorage，且 embedded 流程 token 入 URL（已验证）
- **位置**: `frontend/src/stores/auth.ts:317`、`frontend/src/utils/embedded-url.ts:29`
- **CWE-922/312** · 验证: 任何 XSS（含 H-5 修复前的残留面）可直接读走双 token；`buildEmbeddedUrl` 把 token 拼进 URL → 浏览器历史、代理日志、Referer 泄露
- **修复**:
  1. access token 迁移到 `sessionStorage`（或 httpOnly cookie + CSRF token），至少 refresh_token 不出 localStorage；
  2. embedded 场景改为一次性 URL 参数换 cookie/session（`/auth/embed?code=...` 后端换发，前端不持久化原始 token）；
  3. 短期缓解：CSP `script-src 'self'` 已存在（`security.csp` 默认开）保持开启，并对含 token 的 URL 禁用 `rel` 外泄（`<a rel="noopener noreferrer">`）。

### H-7 · 上游 base_url SSRF（admin 级）+ 默认允许私网（已验证）
- **位置**: `handler/admin/account_handler.go:1086`（base_url 仅 admin 可配）→ `service/gateway_upstream_request.go:906`；`config.go:2071`（`url_allowlist.allow_private_hosts` 默认 true）
- **CWE-918** · 验证: admin（或 admin 账号被入侵者）可把任意上游 base_url 指向 `http://169.254.169.254/`、内网服务；allowlist 关闭时私网全放行。自托管上游（本机 Ollama/llama.cpp）是常见合法需求，故默认放行有合理性，但属**已知可控面**
- **修复**: 文档化该风险 + 对 base_url 抓取复用 C-1 的 SSRF 安全 dialer（允许私网时仅放行 `allow_private_hosts` 白名单网段）；运维侧建议部署时显式 `allow_private_hosts=false` 并把需要的自托管网段加白。

---

## MEDIUM

| # | 漏洞 | 位置 | 修复要点 |
|---|---|---|---|
| M-1 | OAuth 自动建账把 access/refresh token 放 302 URL fragment 传递（linuxdo/wechat/dingtalk/oidc 同构） | `handler/auth_linuxdo_oauth.go:822` | fragment 不入服务端日志，但会进前端 URL 状态；改为后端 session 暂存 token，前端 302 落地后用一次性 code 换发 |
| M-2 | Passkey 注册仅要求密码，未走 TOTP step-up → 被劫持会话可种永久免密凭证 | `handler/passkey_handler.go:131`、`service/passkey.go:158` | 注册/修改 passkey 挂 `stepUpAuth` 中间件（与改密同级的敏感操作） |
| M-3 | 菜单 `custom_menu_items.url` 未消毒 → 开放重定向 / iframe 嵌入钓鱼 | `views/user/CustomPageView.vue:105` | 统一走 `sanitizeUrl`（scheme 白名单 http/https + 相对路径），禁止 `javascript:`/`data:` |
| M-4 | Failover 重试放大：1 个用户请求 → N 次上游调用（N=账号切换上限） | `handler/failover_loop.go:54`、`gateway_handler.go:518` | 对按量计费上游（Anthropic/OpenAI）在切换前对"已消耗 token"做预扣或上限记账；或降低 `maxAccountSwitches` 默认值并在 ops 面板可见 |
| M-5 | `sanitizeSvg` 允许 `foreignObject` → SVG 内 XSS（侧边栏 icon 等） | `frontend/src/utils/sanitize.ts:3` | 白名单去掉 `foreignObject`/`script`/事件属性，或统一走 DOMPurify（`USE_PROFILES: {svg: true, svgFilters: true}`） |
| M-6 | 负值兑换码 `GREATEST(0)` 钳位与 `AdjustBalance` 语义分叉 → 扣减类码行为不一致 | `service/redeem_service.go:492` vs `user_repo.go:866` | 统一走 `ApplyRedeemBalanceAdjustment` 原子路径，删除双实现 |
| M-7 | WebSocket Live 边带 `InsecureSkipVerify:true` 连带关闭 Origin 校验 | `handler/openai_live.go:239` | TLS 校验与 Origin 校验解耦：自签场景用 RootCAs 指定，Origin 仍按 `security.allowed_origins` 校验 |
| M-8 | 支付 webhook 校验失败 / probe 解析错误回显含订单号、内部 URL 的片段 | `handler/payment_webhook_handler.go:108`、`proxy_probe_service.go:174` | 对客户端可见的错误做白名单化文案，细节只进服务端日志 |
| M-9 | `promptResultCache` key 含 `endpoint.Token`（敏感串进缓存 key/日志面）+ 结果抖动侧信道 | `securityaudit/prompt_result_cache.go:145` | key 改为 `sha256(endpoint_id + prompt_hash)`，token 永不入 key |

## LOW

| # | 漏洞 | 位置 | 修复要点 |
|---|---|---|---|
| L-1 | Step-up 授权对无 `sid` 的旧 JWT 回退用户级 key `u<id>` → 一个会话 sudo 全局生效 | `middleware/step_up.go:32` | 旧 token 不授予用户级 step-up（强制刷新后走会话级），或把回退 TTL 压到 5min |
| L-2 | Cookie `Secure` 由 `X-Forwarded-Proto` 启发式推导，代理未传该头时 HTTPS 站下发非 Secure cookie | `handler/auth_linuxdo_oauth.go:1012` | 以 `server.trusted_proxies`/显式 `security.cookie_secure` 配置为准，启发式仅作 fallback |
| L-3 | `proxy_probe.insecure_skip_verify=true` 被接受但探测时硬失败（语义矛盾、可用性损失） | `pkg/httpclient/pool.go:126`、`proxy_probe_service.go:24` | 统一：probe 与业务连接同策略，或 probe 明确支持该开关 |
| L-4 | 插件二进制 exec 依赖 ed25519 签名 + 路径规范化，残余发布者公钥信任风险 | `service/plugin_runtime.go:42`、`plugin_package.go:352` | 文档化信任模型；管理端展示插件发布者指纹供人工确认 |
| L-5 | 迁移 `DROP INDEX CONCURRENTLY IF EXISTS %s` 字符串拼接（非用户可控，维护隐患） | `repository/migrations_runner.go:605,399` | 索引名白名单（`^[a-z_][a-z0-9_]*$`）再拼接 |
| L-6 | 宽松 JSON 读取路径 `readLenientJSONRequestBodyWithPrealloc` 未套用 `TextMaxBodySize` → 体大小限制旁路（内存面） | `pkg/httputil/body.go:86` | 该路径同样包 `io.LimitReader(maxBody)` |
| L-7 | 流式 failover 指纹分叉 / WS 稳定 ID 复用可能导致重复记账（低频） | `handler/openai_live.go:827`、`gateway_usage_billing.go:251` | 记账幂等键统一为 `request_id + attempt_generation`，Redis SETNX 兜底 |

## INFO（确认无问题/卫生项）

- **无 SQL 注入**: 所有 `ORDER BY` 走列名白名单 switch，值走 `$N` 参数（`usage_log_repo_query.go:265`、`ops_repo.go`、`channel_repo.go`）
- **无路径穿越**: 插件 zip/tar 解包与 page handler 均有 jail 校验
- **无命令注入**: `pg_dump`/`psql`/deployer 均为 argv 数组、无 shell 插值；插件二进制经 SHA256+签名校验
- **认证面整体干净**: JWT 仅 HMAC 白名单算法、exp/nbf/TokenVersion 全校验；OAuth state 走 cookie 比对 + `crypto/rand`；webhook 全 HMAC；**未发现 IDOR/越权**（S1 全量核对过用户级 handler 的属主检查）
- **卫生**: JWT parser 白名单收窄到仅 HS256（只签发 HS256）；`OptionalJWTAuth` 已 fail-closed；图片并发限流为进程内实现，水平扩容时建议迁移 Redis（S3-14）

---

## 修复优先级建议

1. **本周**: C-1（SSRF，1 个共享 SafeDialer 覆盖 Kiro/base_url/probe 三处）、H-2（改默认值 1 行 + 文档）、H-3（token_version 列 + 1 个迁移）
2. **两周内**: H-1（扣款下限 SQL + 透支告警）、H-4（TOTP SETNX）、H-5（HomeView 一行 DOMPurify）、H-6（token 存储迁移）
3. **排期**: M-1~M-9、L-1~L-7 随常规迭代
4. **回归测试**: 每条修复附最小回归用例（SSRF 用 httptest 内网 mock；XFF 用带 header 的集成测试；透支用并发扣款测试；TOTP 用重放用例）
