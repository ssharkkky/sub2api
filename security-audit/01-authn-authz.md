# AuthN/AuthZ & Session Security Audit

**Scope:** `backend/internal/server/middleware/` (jwt_auth, api_key_auth, api_key_auth_google, admin_auth, admin_compliance, admin_only, optional_jwt_auth, step_up, session_binding, backend_mode_guard, auth_subject, cors, panel_rate_limit, ingress_reject, security_headers, client_request_id, request_body_limit), `backend/internal/handler/` (user_handler, api_key_handler, totp_handler, passkey_handler, auth_dingtalk_client, auth_linuxdo_oauth, redeem_handler, subscription_handler, composite_platform, page_handler), `backend/internal/server/routes/` (auth, admin, user, common, payment, gateway + router.go), `backend/internal/service/` (auth_service, user, api_key_service, totp_service, passkey, redissession, oauth)

**Stack:** Go + Gin + ent ORM + PostgreSQL + Redis, Vue 3 frontend. Auth: JWT access tokens (HS256, `Authorization: Bearer`), per-user API keys (`sk-*`, `x-api-key`/`x-goog-api-key`/`Authorization: Bearer`), refresh token family in Redis, TOTP (pquerna/otp), WebAuthn passkeys, OAuth (LinuxDo/GitHub/Google/WeChat/OIDC/DingTalk).

**Date:** 2026-09-02
**Auditor model:** muse-spark-1.2

---

## Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH | 2 |
| MEDIUM | 3 |
| LOW | 2 |
| INFO | 3 |
| **Total** | **10** |

**Top risks:**

- **Stateless JWT revocation gap (HIGH):** `POST /api/v1/auth/revoke-all-sessions` and `RevokeAllUserSessions` delete only Redis refresh-token families; stateless `access_token` (default 24 h) stays valid up to expiry. Stolen access token survives explicit logout.
- **TOTP code replay within time window (HIGH):** `totp.Validate` is called without a single-use / recent-code cache. Same 6-digit code can be replayed multiple times within its 30 s (effectively 90 s with default skew) to satisfy `Login2FA` or `StepUp` more than once.
- **OAuth token-in-fragment delivery (MEDIUM):** Auto-created OAuth users receive `access_token` + `refresh_token` via 302 `Location: <frontend>#access_token=...&refresh_token=...`. Fragment tokens persist in browser history, extensions, and shoulder-surfing context.
- **No high-impact IDOR or privilege escalation on in-scope handlers was confirmed.** All user-scoped handlers (`api_key_handler`, `usage_handler`, `image_playground_handler`, `user_handler`, `redeem/subscription`) correctly gate on `GetAuthSubjectFromContext` / `apiKey.UserID != subject.UserID` or `VerifyOwnership`. Admin routes are uniformly `adminAuth` + `AdminComplianceGuard`.
- **JWT and API-key middleware are otherwise sound** (HS* allow-list + `*jwt.SigningMethodHMAC` check, `maxTokenLength` 8192, `maxAPIKeyAuthorizationHeaderBytes` 256, `subtle.ConstantTimeCompare` for admin API key and email codes, open-redirect sanitizer, state + PKCE on OAuth).

---

## Route & Middleware Inventory (verified)

**Global (router.go:60-77):** `BeginRequest(drain)` → `RequestLogger` → `SessionBindingContext` (IP+UA → `service.WithSessionBinding`) → `Logger` (health/status excluded, `X-Request-ID` injected) → `CORS` → `SecurityHeaders` (CSP nonce, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`) → `ServerTiming` → `ServeEmbeddedFrontend`. No auth at this layer.

**Common (routes/common.go):** `GET /health`, `POST /api/event_logging/batch`, `GET /setup/status` — public, no auth (intentional).

**Auth (routes/auth.go):** `v1.Group(/auth)` → `BackendModeAuthGuard` + `AuditLog` (auth events). Public: `POST /register` (+ `registration-success` 5/24h, fail-close), `POST /login` (20/min), `POST /login/2fa`, `POST /passkey/login/begin|finish`, `POST /send-verify-code`, `POST /refresh`, `POST /logout`, `POST /validate-promo-code|invitation`, `POST /forgot-password|reset-password`, `GET/POST /oauth/*/{start,callback,complete-registration,create-account,bind-login}` (per-provider rate limits), `POST /oauth/pending/{exchange,send-verify-code,create-account,bind-login}`. Authenticated sub-group `/auth/me`, `/auth/revoke-all-sessions`, `/auth/oauth/bind-token` → `jwtAuth` + `BackendModeUserGuard` + `Global` rate limit.

**User (routes/user.go):** `v1.Group("")` → `jwtAuth` → `BackendModeUserGuard` → `Global` → `AuditLog`. Sub-groups: `/user/profile|password|aff|account-bindings|notify-email|totp/*|passkeys/*`, `/keys` (CRUD + available/groups + rates), `/channels/available`, `/image-playground/{options,tasks}` (tasks `POST` additionally `RequestBodyLimit` → `ResolveAPIKey` → `apiKeyAuth` → `Submit`), `/usage/*` (+ `Heavy`), `/announcements`, `/redeem`, `/subscriptions`, `/channel-monitors`. All handlers gate on `GetAuthSubjectFromContext`.

**Model Plaza (routes/model_plaza.go) / (router.go:135):** `optionalJWTAuth` — anonymous allowed; when `Authorization` present it delegates to strict `jwtAuth` (fail-closed on 401, so frontend refresh flow still triggers). Unauthenticated path still benefits from `Global` limiter when authenticated; anonymous path rate-limited by public-IP limiter only where applied. No privilege issue.

**Admin (routes/admin.go):** `v1.Group(/admin)` → `adminAuth` (JWT admin or `x-api-key` admin API key) → `Global` → `AuditLog` → `AdminComplianceGuard`. Sub-routes listed `14:39` — all inherit the stack. Step-up (`stepUpAuth`) applied selectively: `GET /accounts/data` (export), `GET /proxies/data` (export), all `/backups/*`, `/data-management/s3/*`, `/plugins/*`. Verified `AdminOnly` is **not** used as standalone on new routes (adminAuth already enforces `IsAdmin()`), and `AdminComplianceGuard` correctly exempts `/admin/compliance` itself.

**Gateway (routes/gateway.go):** `/v1` → `bodyLimit` → `ClientRequestID` → `OpsErrorLogger` → `InboundEndpoint` → `apiKeyAuth` (main) or `APIKeyAuthWithSubscriptionGoogle` for `/v1beta`. Composite resolvers (`CompositeRouteResolver`) run **after** `apiKeyAuth` (so `apiKey.Group` is available) and read `model` from JSON/multipart body *after* auth; no auth bypass. `RequireGroupAssignment` rejects ungrouped keys when `setting.IsUngroupedKeySchedulingAllowed()==false`. `ForcePlatform` for `/antigravity/*`. Root aliases (`/responses`, `/chat/completions`, `/images/*`, `/videos/*`, etc.) duplicate the same `bodyLimit + clientRequestID + opsErrorLogger + endpointNorm + apiKeyAuth + compositeTarget + requireGroup` chain — verified no alias missing auth.

**Payment (routes/payment.go):** `v1.Group(/payment)` → `jwtAuth` + `BackendModeUserGuard` + `Global` for user-facing; `v1.Group(/payment/public)` intentional public — `VerifyOrderPublic` + `ResolveOrderPublicByResumeToken` (signed resume-token, not session); `v1.Group(/payment/webhook)` — `EasyPay|Alipay|Wxpay|Stripe|Airwallex` — no auth, HMAC/signature verified in handler; `v1.Group(/admin/payment)` → `adminAuth` + `AuditLog` + `AdminComplianceGuard`.

**Pages (handler/page_handler.go:261):** `v1.Group(/pages)` → `jwtAuth` for `GET /:slug` (`GetPageContent` + `checkSlugVisibility` dual-gate); second `v1.Group(/pages)` — no auth for `GET /:slug/images/*filename` (`ServePageImage` → `checkImageSlugVisibility` blocks `admin`-only pages for anonymous); `v1.Group(/pages)` → `adminAuth` + `AdminComplianceGuard` for `GET ""` (`ListPages`). No missing auth.

**CORS (middleware/cors.go):** `allowAll` only if `*` in `allowed_origins`; if both `*` and explicit origins → collapses to `*` with warning; `allowAll && allowCredentials` → forces `allowCredentials=false` (prevents `Access-Control-Allow-Origin: *` with credentials). Origin check is exact `map[string]struct{}`. `Access-Control-Allow-Methods: POST, OPTIONS, GET, PUT, DELETE, PATCH`, expose `ETag, Server-Timing`, max-age 86400. Preflight returns 204 if allowed else 403. No `Vary: Origin` omission — correctly added when reflecting explicit origin.

**Panel rate limit (middleware/panel_rate_limit.go):** `Global()` / `Heavy()` are user-scoped (`panel:{scope}:user:{id}`) via `GetAuthSubjectFromContext`; fail-open on Redis error; admin exempt controlled by `setting.PanelRateLimitSettings.ExemptAdmin`. `PublicIP()` is IP-scoped with `SecurityClientIP`; loopback/private/link-local/unspecified → skip (avoids collapsing all reverse-proxy traffic into one bucket). No auth bypass: missing subject → pass-through (intended for unauthenticated public settings, which have explicit `PublicIP()` on their own group).

---

## Findings

### S1-1 [HIGH] TOTP codes are replayable within their validity window — no single-use enforcement

- **File:** `backend/internal/service/totp_service.go:332`, `:383`, `:408`
- **CWE:** CWE-308 (Use of Single-factor Authentication), CWE-307 (Improper Restriction of Excessive Authentication Attempts — partial)
- **Description:** Both `VerifyCode` (login 2FA and step-up) and `CompleteSetup` call `totp.Validate(code, secret)` from `github.com/pquerna/otp` with default period 30 s and skew 1 (effective 90 s window) and never record that a code has been used. The same 6-digit code can be presented multiple times within the same time-step to call `Login2FA` for several token pairs or to call `POST /api/v1/user/totp/step-up` repeatedly, and an eavesdropped code (proxy logs, shoulder surf, clipboard) can be replayed before expiry. The existing brute-force counter (`IncrementVerifyAttempts`, `maxTotpAttempts=5` per 15 min) is cleared on success (`ClearVerifyAttempts`), so a successful replay does not consume the failure budget.
- **Vulnerable code:**

```go
// totp_service.go:383
valid := totp.Validate(code, secret) // period 30s, skew 1
if !valid {
    _, _ = s.cache.IncrementVerifyAttempts(ctx, userID)
    return ErrTotpInvalidCode
}
_ = s.cache.ClearVerifyAttempts(ctx, userID)
return nil

// totp_service.go:248 — same during setup
if !totp.Validate(totpCode, session.Secret) {
    return ErrTotpInvalidCode
}

// totp_service.go:408 — step-up delegates without replay check
func (s *TotpService) VerifyStepUp(ctx context.Context, userID int64, sessionKey, code string) (time.Duration, error) {
    if err := s.VerifyCode(ctx, userID, code); err != nil { return 0, err }
    if err := s.cache.SetStepUpGrant(ctx, userID, sessionKey, StepUpGrantTTL); err != nil { ... }
    return StepUpGrantTTL, nil
}
```

- **Data flow / attack scenario:**

  1. Victim `POST /api/v1/auth/login` with correct password → server detects `TOTPEnabled`, returns `{requires_2fa:true, temp_token:<32B hex>}`. The `temp_token` session lives `totpLoginTTL=5 min` in Redis.
  2. Victim `POST /api/v1/auth/login/2fa` with `{temp_token, totp_code:"123456"}`. Attacker who sniffed `totp_code` (e.g., corporate TLS inspection, malicious browser extension reading form, or shoulder video) copies it within the same 30 s step.
  3. Attacker concurrently `POST /api/v1/auth/login/2fa` with the same `{temp_token (stolen or second session) and totp_code}` — or more realistically, after victim succeeds, attacker calls `POST /api/v1/user/totp/step-up` with the intercepted code (any authenticated hijacked session cookie/token) to obtain a 15 min `StepUpGrant` for high-value admin ops (account/proxy export, backup download, plugin upload).
  4. Because `Validate` is stateless, both calls succeed. With default `skew=1`, code is valid for `t-30..t+30` → ≈ 60–90 s replay window depending on clock alignment.

  No `TotpCache` method stores used codes; `grep -rn TotpCache` confirms only `SetupSession, LoginSession, VerifyAttempts, StepUpGrant` — no `UsedCodes`.

- **Impact:** Second-factor bypass via code reuse. Turns TOTP from one-time into N-time within window; extends an intercepted code's utility across multiple sessions. Combined with S1-2, an attacker who phishes one TOTP code during login can mint both a full session and a sudo grant.
- **Fix:** Add a Redis-backed single-use cache keyed by `user:{id}:totp_code:{hash(code)}` or `user:{id}:totp_counter` (using `totp.ValidateCustom` with explicit `now` and extracting the matched period counter). Example diff:

```go
// service/totp_service.go — new method on TotpCache:
//   SetUsedCode(ctx context.Context, userID int64, codeHash string, ttl time.Duration) (bool, error)
//   // returns true if newly set (SET NX), false if already present
const totpReplayTTL = 90 * time.Second // period + skew margin

func (s *TotpService) VerifyCode(ctx context.Context, userID int64, code string) error {
    // ... rate limit + fetch user + decrypt ...
    code = strings.TrimSpace(code)
    if len(code)!=6 { return ErrTotpInvalidCode }

    // Anti-replay: hash the code with user-scope
    h := sha256.Sum256([]byte(strconv.FormatInt(userID,10)+":"+code))
    hashHex := hex.EncodeToString(h[:])
    ok, err := s.cache.SetUsedCode(ctx, userID, hashHex, totpReplayTTL)
    if err != nil { return infraerrors.InternalServer("TOTP_REPLAY_CHECK_FAILED", "...") }
    if !ok {
        // Code already seen in this window → treat as invalid, count as attempt
        _, _ = s.cache.IncrementVerifyAttempts(ctx, userID)
        return ErrTotpInvalidCode
    }

    // Use ValidateCustom to pin time and avoid double-validation surprises
    valid, err := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
        Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
    })
    if err != nil || !valid {
        // On failure, remove the replay marker so user can retry a *different* code
        // (or keep it — see note below); either way do not clear on failure
        _, _ = s.cache.IncrementVerifyAttempts(ctx, userID)
        // Optional: delete the marker on invalid so a typo doesn't burn a valid future code
        _ = s.cache.DeleteUsedCode(ctx, userID, hashHex)
        return ErrTotpInvalidCode
    }
    _ = s.cache.ClearVerifyAttempts(ctx, userID)
    return nil
}
```

  Implement `SetUsedCode` as `SET key value NX EX 90` in Redis and `DeleteUsedCode` as `DEL`. Alternative stronger design: store the TOTP time-step counter (integer dividing Unix timestamp by 30) and reject if `counter <= lastUsedCounter[userID]` (RFC 6238 §5.2). Either approach eliminates replay.

- **Confidence:** high. Verified `pquerna/otp` docs: `Validate` is pure function of `code + secret + time`, no state. Searched codebase for any replay map — none.

---

### S1-2 [HIGH] `RevokeAllSessions` / `Logout` deletes only refresh families — stateless access tokens survive to expiry (default 24 h)

- **File:** `backend/internal/service/auth_service.go:1894` (`RevokeAllUserSessions`, `RevokeAllUserTokens`), `backend/internal/handler/auth_handler.go:757` (`RevokeAllSessions`), `backend/internal/config/config.go:2276` (default `jwt.expire_hour=24`)
- **CWE:** CWE-613 (Insufficient Session Expiration), CWE-287
- **Description:** Access tokens are stateless HS256 JWTs validated only against `jwt.secret` + `exp` + `TokenVersion` fingerprint (derived from `email\npassword_hash`). `RevokeAllUserTokens` intentionally notes `// users 表没有 token_version 列 … 之前紧跟其后的整行 Update 不写任何有效数据，却会用旧快照覆盖…故已移除` (`auth_service.go:1904-1907`) and only calls `RevokeAllUserSessions` → `DeleteUserRefreshTokens` (Redis). No Redis/JTI denylist or `exp`-aware blocklist is written. A stolen `Authorization: Bearer <access_token>` stays valid until `exp` (24 h default, configurable up to 168 h). The same applies to the legacy `AuthService.RefreshToken` path (single JWT without refresh family) and to the `adminAuth` WebSocket `jwt.<token>` subprotocol — admin tokens also escape revocation.
- **Vulnerable code:**

```go
// auth_service.go:1904
func (s *AuthService) RevokeAllUserTokens(ctx context.Context, userID int64) error {
    if _, err := s.userRepo.GetByID(ctx, userID); err != nil { return fmt.Errorf("get user: %w", err) }
    if err := s.RevokeAllUserSessions(ctx, userID); err != nil { ... }
    return nil // ← no access-token invalidation
}

func (s *AuthService) RevokeAllUserSessions(ctx context.Context, userID int64) error {
    if s.refreshTokenCache == nil { return nil }
    return s.refreshTokenCache.DeleteUserRefreshTokens(ctx, userID)
}

// handler/auth_handler.go:757
func (h *AuthHandler) RevokeAllSessions(c *gin.Context) {
    subject, ok := middleware2.GetAuthSubjectFromContext(c)
    if !ok { response.Unauthorized(c,...); return }
    if err := h.authService.RevokeAllUserTokens(c.Request.Context(), subject.UserID); err != nil { ... }
    response.Success(c, RevokeAllSessionsResponse{Message: "All sessions have been revoked. ..."})
}

// jwt_auth.go:86 — validates against password-hash fingerprint, not a revocation list
if claims.TokenVersion != user.TokenVersion { // derived as sha256(email + "\n" + passwordHash)[0:8] ^ storedVersion
    AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked (password changed)")
    return
}
// auth_service.go:1660 — password reset is the *only* path that invalidates access tokens (because password_hash changes the fingerprint)
user.PasswordHash = hashedPassword
user.TokenVersion++
if err := s.userRepo.Update(ctx, user, UserUpdateFields{PasswordHash: true}); err != nil { ... }
```

- **Data flow / attack scenario:**

  1. Attacker obtains short-lived `access_token` via XSS exfiltrating `localStorage`, malicious dependency reading `Authorization` header, or log leak. Default `jwt.expire_hour=24` (so `exp = now + 24h`, or `jwt.access_token_expire_minutes` if set). Refresh family `rt_<64 hex>` is separate.
  2. Victim notices hijack, calls `POST /api/v1/auth/revoke-all-sessions` with valid JWT → server replies `200 "All sessions have been revoked. Please log in again."` and `DELETE rt_*` families in Redis.
  3. Attacker continues `GET /api/v1/auth/me` with the stolen access token every minute for up to 24 h; `jwt_auth.go` validates HMAC, `TokenVersion` still matches (password unchanged), `IsActive()` true, `enforceSessionBinding` disabled by default → `200`.
  4. If attacker also stole `refresh_token`, it is now invalid (deleted), but attacker does not need it — the access token alone is sufficient for all `jwtAuth`-gated panel APIs (profile, keys, usage, admin if victim is admin). Admin access persists for 24 h even after victim rotates password? No — password rotation *does* rotate the fingerprint and invalidates; but revoke without rotation does not.

  `POST /api/v1/auth/logout` (public, optional `refresh_token` body) has same gap — calling without a `refresh_token` is a no-op for access tokens.

- **Impact:** Revocation UI/UX promises full session kill but delivers partial. Theft of one access token = 24 h panel access persistence, including admin panel if victim is admin. Mass logout after suspected breach is ineffective unless victims also change passwords.
- **Fix — choose one, in order of strength:**

  **Option A (recommended, minimal churn): JWT denylist keyed by `jti`/`sid` with TTL = remaining `exp`:**

```go
// auth_service.go — add `sid` (SessionID) as JWT ID, already emitted in generateAccessToken
type JWTClaims struct { ... SessionID string `json:"sid,omitempty"` ... }

func (s *AuthService) RevokeAllUserTokens(ctx context.Context, userID int64) error {
    if _, err := s.userRepo.GetByID(ctx, userID); err != nil { return err }
    // Existing: delete refresh families
    _ = s.RevokeAllUserSessions(ctx, userID)
    // NEW: bump denylist marker; jwt_auth checks this before accepting a token
    // Store e.g. `revoked_access:user:{id}` = nowUnix, TTL = max JWT lifetime (24h+clock skew)
    revokeAt := time.Now().Unix()
    return s.refreshTokenCache.SetUserAccessRevokedAt(ctx, userID, revokeAt, time.Duration(s.cfg.JWT.ExpireHour)*time.Hour + 5*time.Minute)
}

// jwt_auth.go — after ValidateToken + before TokenVersion check:
if revokedAt, err := authService.GetUserAccessRevokedAt(c.Request.Context(), claims.UserID); err==nil && revokedAt>0 {
    if claims.IssuedAt != nil && claims.IssuedAt.Unix() <= revokedAt {
        AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked")
        return
    }
}
```

  Add `RefreshTokenCache` methods `SetUserAccessRevokedAt`/`GetUserAccessRevokedAt` backed by `SET jwt:revoke:user:{id} <ts> EX <maxTTL>` and read with `GET`. Also add per-token `BlacklistJTI(sid)` for single-logout (`Logout` with `refresh_token`) if desired. `ValidateToken` should populate `jti = sid` for per-token revoke.

  **Option B (also valid): Shorten default access lifetime** — change `viper.SetDefault("jwt.expire_hour", 24)` → `1` and `SetDefault("jwt.access_token_expire_minutes", 15)`. Reduces window from 24 h to 15 min without code, but does not fix the semantic gap. Should be done regardless.

  **Option C (stronger, more invasive): Switch access tokens to opaque Redis-backed sessions** — store `sid` → user+meta in Redis and validate existence. Eliminates stateless revocation problem entirely, at cost of per-request Redis lookup (already done for refresh flow).

  Front-end should also clear `localStorage` on revoke, but server-side fix is required — client clearance is not a defense.

- **Confidence:** high. Traced `RevokeAllUserTokens` → `RevokeAllUserSessions` → `DeleteUserRefreshTokens` and confirmed no `Store`/`Set` of an access-token blocklist and no `sid`/`jti` check in `jwtAuth`/`ValidateToken`. Verified `config.go:2276` default 24 h and no `exp`-shortening elsewhere.

---

### S1-3 [MEDIUM] OAuth access/refresh tokens delivered via URL fragment (302 `Location: ...#access_token=...&refresh_token=...`) — persisted in history, extensions, screenshots

- **File:** `backend/internal/handler/auth_linuxdo_oauth.go:822` (`redirectOAuthTokenPair`), `auth_wechat_oauth.go` (parallel), `auth_dingtalk_oauth.go`, `auth_oidc_oauth.go`, `auth_github/oauth` etc. — all share `redirectWithFragment` at `auth_linuxdo_oauth.go:851`.
- **CWE:** CWE-598 (Use of GET Request Method With Sensitive Query Strings) — fragment variant; CWE-200
- **Description:** When OAuth callback auto-creates a user (no compat-email collision and no invitation/verification gates), handler calls `redirectOAuthTokenPair(c, frontendCallback, tokenPair, redirectTo)`, which builds `url.Values{access_token, refresh_token, expires_in, token_type, redirect}` and assigns `u.Fragment = fragment.Encode()` then `c.Redirect(302, u.String())`. The browser receives `302 Location: https://frontend.example.com/auth/linuxdo/callback#access_token=eyJ...&refresh_token=rt_...`. Fragments are *not* sent to the server on the next request, but they **are** retained in: browser history (across sessions), `window.location.hash` readable by any JS on the page (including third-party scripts/ads), browser extensions with `tabs` permission, OS clipboard if user copies link, and screenshots/recordings. `refresh_token` (30 d, `rt_` prefix, single-use but rotates) is also leaked — enables account takeover if history is read within its lifetime. `redirectWithFragment` does `Cache-Control: no-store` on the 302 itself, but the *landing* page (`frontendCallback`) is whatever the admin configured (often CDN-cacheable SPA), and fragment persists client-side indefinitely.
- **Vulnerable code:**

```go
// auth_linuxdo_oauth.go:822
func redirectOAuthTokenPair(c *gin.Context, frontendCallback string, tokenPair *service.TokenPair, redirectTo string) {
    fragment := url.Values{}
    if tokenPair != nil {
        fragment.Set("access_token", truncateFragmentValue(tokenPair.AccessToken))
        fragment.Set("refresh_token", truncateFragmentValue(tokenPair.RefreshToken))
        fragment.Set("expires_in", strconv.Itoa(tokenPair.ExpiresIn))
        fragment.Set("token_type", "Bearer")
    }
    // ... redirect param also truncated to 512
    redirectWithFragment(c, frontendCallback, fragment)
}
func redirectWithFragment(c *gin.Context, frontendCallback string, fragment url.Values) {
    u, err := url.Parse(frontendCallback)
    // scheme check only for http/https, otherwise fallback to /dashboard
    u.Fragment = fragment.Encode()
    c.Header("Cache-Control", "no-store")
    c.Header("Pragma", "no-cache")
    c.Redirect(http.StatusFound, u.String())
}
```

  Contrast the non-auto-create path which uses the pending-session double-cookie flow (`Set-Cookie: oauth_pending_session=...; HttpOnly; Path=/api/v1/auth/oauth; Max-Age=600`) + fragment `error` only — that path **does not** leak tokens. The fragment-token path is taken only for "happy path" auto-creation (`LoginOrRegisterOAuthWithTokenPairAndPromoCode` succeeds, no invitation required). 

- **Data flow / attack scenario:**

  1. Victim `GET /api/v1/auth/oauth/linuxdo/start?redirect=/dashboard` → receives `302 https://linuxdo.example.com/authorize?state=...` plus `Set-Cookie: linuxdo_oauth_state, _verifier, _redirect, oauth_pending_browser_session` (HttpOnly, 600 s).
  2. Victim authorizes → provider `GET /api/v1/auth/oauth/linuxdo/callback?code=...&state=...` → server validates state/PKCE, fetches `userinfo`, finds no existing identity and `!emailVerificationRequired && !forceEmailOnSignup` → auto-creates user + `GenerateTokenPair` → `302 https://app.example.com/auth/linuxdo/callback#access_token=eyJhbG...&refresh_token=rt_abc...&expires_in=86400`.
  3. Attacker who later gains read access to victim's browser history (stolen device, malware with `history` API, or victim pastes the URL into a support ticket/screenshot) extracts tokens from the fragment. `access_token` valid 24 h, `refresh_token` valid 30 d and rotates in Redis but the captured `refresh_token` is still valid until first use/rotation.
  4. Secondary leak: if the landing page includes any third-party JS (Cloudflare Insights `https://static.cloudflareinsights.com`, Tencent Captcha domains whitelisted in CSP — see `security_headers.go:20`), that script can read `location.hash` on load. CSP currently allows those scripts; compromise of any would silently harvest OAuth tokens.

- **Impact:** Confidential tokens enter a URL-persistent client-side store with weak access control. Compared to the `oauth_pending_*` HttpOnly cookie pattern used elsewhere in this codebase, the fragment path is the weaker variant. CVSS-like: Low prevalence (only auto-created OAuth, not the pending-choice branch) but high impact when hit.
- **Fix:** Deliver tokens out-of-band instead of fragment. The codebase already has a secure pattern: `createOAuthPendingSession` + `oauth_pending_session` HttpOnly cookie + `POST /api/v1/auth/oauth/pending/exchange` (or pending-DTO polled by frontend). Use that for *all* OAuth completions, not just choice flows. Concrete change:

```go
// Replace in LinuxDoOAuthCallback (and parallel providers) the direct redirectOAuthTokenPair branch:
 // BEFORE:
 h.authService.RecordSuccessfulLogin(c.Request.Context(), user.ID)
 clearOAuthPendingSessionCookie(c, secureCookie)
 clearOAuthPendingBrowserCookie(c, secureCookie)
 redirectOAuthTokenPair(c, frontendCallback, tokenPair, redirectTo)

// AFTER:
if err := h.createOAuthPendingSession(c, oauthPendingSessionPayload{
    Intent:         oauthIntentLogin,
    Identity:       identityKey,
    TargetUserID:   &user.ID,
    ResolvedEmail:  user.Email,
    RedirectTo:     redirectTo,
    BrowserSessionKey: browserSessionKey,
    UpstreamIdentityClaims: upstreamClaims,
    CompletionResponse: map[string]any{"redirect": redirectTo}, // no tokens here
}); err != nil { redirectOAuthError(c, frontendCallback, "session_error", "...", ""); return }
// Immediately mint tokens *server-side* and store them under the pending session
// (extend PendingAuthSession.CompletionResponse to hold tokens encrypted at-rest, or
// use a short-lived Redis key `oauth:token:{pendingToken}` with the token pair).
// Then:
redirectToFrontendCallback(c, frontendCallback) // no fragment
// And frontend does: POST /api/v1/auth/oauth/pending/exchange {pending_token} → {access_token, refresh_token} over XHR (Authorization not needed, cookie proves possession)
```

  If keeping fragment is unavoidable (deep mobile integration), at least: (a) mark landing route `Cache-Control: no-store`, (b) add `<meta name="referrer" content="no-referrer">`, (c) front-end must `history.replaceState(null,"", location.pathname+location.search)` immediately on load to strip the fragment *before* any third-party script runs, and (d) do not include `refresh_token` — only a one-time `exchange_code` that POST-exchanges over TLS. Existing `POST /api/v1/auth/oauth/pending/exchange` already rate-limited at 20/min — reuse it.

- **Confidence:** high. Verified the two OAuth callback branches by reading `LinuxDoOAuthCallback:297-395` end-to-end (and analogous `WeChat/DingTalk/OIDC`). Verified `redirectOAuthError` (fragment `error`) vs `redirectOAuthTokenPair` (fragment tokens) split. Verified `truncateFragmentValue` caps at 512 but does not avoid the persistence issue. Verified CSP (`security_headers.go:14-85`) whitelists third-party scripts on all pages (non-API routes) — so any compromise there can read `location.hash`.

---

### S1-4 [MEDIUM] Default 24 h access-token lifetime amplifies S1-2 and increases stolen-token blast radius

- **File:** `backend/internal/config/config.go:2276-2277` (`viper.SetDefault("jwt.expire_hour", 24)`, `viper.SetDefault("jwt.access_token_expire_minutes", 0)`) and `backend/internal/service/auth_service.go:1428-1435` (fallback to `ExpireHour * 1h`).
- **CWE:** CWE-613
- **Description:** With no explicit `jwt.access_token_expire_minutes`, issued JWTs get `exp = now + 24h` and `nbf = now`. This is at the upper end of the validated range (slog warns only if `>24h`, errors if `>168h`). A 24 h window for a bearer token that is stored in `localStorage` / `Authorization: Bearer` and survives `RevokeAllSessions` is disproportionate: OWASP and NIST recommend 5–15 min for access tokens when refresh rotation exists (and this codebase has refresh rotation via `rt_*` in Redis). Every stolen access token automatically grants 24 h of panel and, if user is admin, 24 h of admin API without re-auth.
- **Vulnerable code:**

```go
// config.go:2276
viper.SetDefault("jwt.expire_hour", 24)
viper.SetDefault("jwt.access_token_expire_minutes", 0) // 0 →回退

// auth_service.go:1428
var expiresAt time.Time
if s.cfg.JWT.AccessTokenExpireMinutes > 0 {
    expiresAt = now.Add(time.Duration(s.cfg.JWT.AccessTokenExpireMinutes) * time.Minute)
} else {
    expiresAt = now.Add(time.Duration(s.cfg.JWT.ExpireHour) * time.Hour) // 24h
}
```

- **Data flow / attack scenario:** See S1-2 steps 1–3. Shortening to 15 min reduces attacker window from 24 h to 15 min automatically, even without adding a denylist.
- **Impact:** Medium on its own (availability of token theft is the root cause), but synergizes with S1-2 to raise its severity from Medium to High. Also increases risk of token leakage via logs/backups: the `auditing` and `ops_error_logs` paths redact tokens but gateway access logs often capture `Authorization` length — with 24 h TTL a leaked log has longer utility.
- **Fix:** Lower the default and document rotation:

```go
viper.SetDefault("jwt.expire_hour", 1) // fallback, but prefer minutes below
viper.SetDefault("jwt.access_token_expire_minutes", 15)
viper.SetDefault("jwt.refresh_token_expire_days", 7) // down from 30 if product allows
```

  And in `config.go:2871-2886` tighten validation: `if c.JWT.ExpireHour > 1 { slog.Warn ... }` and reject `ExpireHour > 24` outright instead of 168. Provide a migration note: already-issued 24 h tokens will naturally expire; no invalidation needed beyond S1-2's denylist.

- **Confidence:** high. Default verified via `SetDefault` and validated by reading `config_test.go` expectations.

---

### S1-5 [MEDIUM] Passkey login and `POST /api/v1/auth/oauth/pending/exchange` intentionally skip TOTP — documented but widens phishing+MFA bypass if passkey enrollment was weakly gated

- **File:** `backend/internal/handler/passkey_handler.go:99` (`FinishLogin` comment), `backend/internal/service/passkey.go:145-146` (WebAuthn config `ResidentKeyRequirementRequired` + `UserVerification: Required`), `backend/internal/handler/auth_handler.go:301` (`Login2FA` flow only for password login)
- **CWE:** CWE-308
- **Description:** `PasskeyHandler.FinishLogin` calls `passkeys.FinishLogin` (usernameless discoverable credential, UV required) and directly `respondWithTokenPair(c, authService, user)` without consulting `user.TotpEnabled`. Comment: *“User verification is mandatory, so a successful passkey assertion already supplies … MFA and does not enter the separate TOTP challenge flow.”* This is **intentional** — WebAuthn with UV is phishing-resistant MFA. The residual risk is during **passkey enrollment**: `BeginRegistration` / `FinishRegistration` gate only on account password (`verifyPasskeyPassword`), not on TOTP step-up. A session hijack (XSS stealing `access_token` for 24 h per S1-2) lets an attacker who also knows or resets the password enroll a new passkey that permanently bypasses TOTP thereafter. Enrollment is rate-limited only via panel `Global` limiter (per-user RPM), not per-enrollment CAPTCHA.
- **Vulnerable code:**

```go
// passkey_handler.go:131
func (h *PasskeyHandler) BeginRegistration(c *gin.Context) {
    subject, ok:= GetAuthSubjectFromContext(c); ...
    creation, token, err := h.passkeys.BeginRegistration(c.Request.Context(), subject.UserID, bindPasskeyPassword(c))
    // only checks password, not TOTP
}
// passkey.go:158
func verifyPasskeyPassword(user *User, password string) error {
    if password == "" { return ErrPasswordRequired }
    if user == nil || !user.CheckPassword(password) { return ErrPasswordIncorrect }
    return nil
}
// passkey_handler.go:99
func (h *PasskeyHandler) FinishLogin(c *gin.Context) {
    // ... BeginLogin handled with VerifyActionCaptchaIfEnabled ...
    user, err := h.passkeys.FinishLogin(c.Request.Context(), req.SessionToken, credentialRequest)
    // ... backendMode check, RecordSuccessfulLogin, respondWithTokenPair
    // ← no CheckPassword / VerifyCode / HasStepUpGrant here
}
```

- **Data flow / attack scenario:**

  1. Attacker phishes victim's password (or reuses a breach) and steals one `access_token` via XSS.
  2. Attacker `POST /api/v1/user/passkeys/register/begin` with `{"password":"<phished>"}` + valid JWT → receives `session_token` + `options`.
  3. Attacker creates a local WebAuthn credential (platform authenticator) and `POST /api/v1/user/passkeys/register/finish` with `{"session_token":…, "credential":…}` → persistent passkey enrolled for victim account.
  4. Victim later enables TOTP expecting second factor on all logins; attacker logs in via `POST /api/v1/auth/passkey/login/begin` (≈ no password) → `finish` → full `access_token + refresh_token` without TOTP, bypassing the newly enabled factor.
  5. Even without passkeys, the OAuth pending flows (`CreatePendingOAuthAccount`/`BindPendingOAuthLogin`) also exchange tokens without TOTP unless `PendingOAuthBindLoginSession` is present (which only happens when a pre-existing user binds and TOTP is challenged elsewhere).

- **Impact:** Downgrade of MFA strength from 2FA to single-factor (passkey possession) — but passkeys themselves are strong, so the net is enrollment-phase confusion rather than immediate account takeover. Medium because it requires prior session hijack + password knowledge.
- **Fix — harden enrollment, keep login as-is:**

```go
func (h *PasskeyHandler) BeginRegistration(c *gin.Context) {
    subject, ok := middleware2.GetAuthSubjectFromContext(c); if !ok { ... }
    // NEW: enrollment requires either (a) TOTP step-up if user.TotpEnabled, or (b) password if not
    user, _ := h.passkeys.UserForCheck(c.Request.Context(), subject.UserID) // or fetch via userService
    if user.TotpEnabled {
        if !middleware.EnforceStepUp(c, h.totpService, h.userService, h.settingSvc) { return }
    } else {
        if err := verifyPasskeyPassword(user, bindPasskeyPassword(c)); err != nil { response.ErrorFrom(c, err); return }
    }
    // ... existing BeginRegistration without re-checking password inside service (pass empty or refactor)
}
```

  Alternatively keep password gate but *also* require `HasStepUpGrant` when `TOTPEnabled`. For `FinishLogin` bypass, add a setting `passkey_skips_totp` default false and document — but current UV=required + resident key design is defensible; the fix above addresses the enrollment vector which is the exploitable path.

- **Confidence:** medium. The login-skip is *documented intent* and WebAuthn UV does count as MFA per NIST AAL2/FIDO guidance; the enrollment-time password-only gate is the actual gap, but it requires an already-authenticated session — not an unauthenticated takeover. Downgrading to MEDIUM for this reason.

---

### S1-6 [LOW] Step-up grants fall back to user-level key (`u<userID>`) for JWTs issued before `sid` existed — one session's sudo upgrades all sessions on old tokens

- **File:** `backend/internal/server/middleware/step_up.go:32` (`StepUpSessionKey`), `backend/internal/service/totp_service.go:408-420`
- **CWE:** CWE-732
- **Description:** `StepUpSessionKey(c, userID)` returns `c.GetString(ContextKeySessionID)` (the `sid` claim) if present, else `fmt.Sprintf("u%d", userID)`. JWTs issued before the `sid`/`bnd` rollout (binding hash `""` path) have empty `SessionID`, so every session for that user shares the single Redis key `stepup:u<id>` (TTL 15 min). A sudo granted in one browser tab/borrowed device instantly sudo-es *all* of the victim's old-token sessions. This weakens the session-binding that step-up is supposed to provide.
- **Vulnerable code:**

```go
func StepUpSessionKey(c *gin.Context, userID int64) string {
    if sid := c.GetString(ContextKeySessionID); sid != "" { return sid }
    return fmt.Sprintf("u%d", userID)
}
```

- **Data flow / attack scenario:** Victim has a long-lived 24 h token from before the `sid` rollout (still valid per S1-2). Attacker steals a *different* access token for the same user (second device) that also lacks `sid`. Victim performs `POST /api/v1/user/totp/step-up` with valid TOTP on their own device → `SetStepUpGrant(ctx, userID, "u123", 15m)`. Attacker, holding the other old token, now passes `HasStepUpGrant(ctx, userID, "u123")` without having supplied TOTP, and can call `POST /api/v1/admin/accounts/data` (export) or `POST /api/v1/admin/backups/:id/restore` if victim is admin.
- **Impact:** Cross-session sudo. Low *now* because new tokens carry `sid` (32 hex chars, 16 random bytes via `hex.EncodeToString(familyBytes)`), but long-lived 24 h tokens from before deployment keep the window open through upgrades (rolling restart does not invalidate old JWTs).
- **Fix:** When `SessionID == ""` and step-up is required, reject with `STEP_UP_REQUIRED` and instruct client to refresh (rotate to a `sid`-bearing token) instead of falling back to user-level. Or, if blocking is too disruptive, keep fallback but scope it narrowly and emit a warning metric:

```go
func StepUpSessionKey(c *gin.Context, userID int64) (string, bool) {
    if sid := c.GetString(ContextKeySessionID); sid != "" { return sid, true }
    return "", false // caller should return STEP_UP_REQUIRED with `refresh_required:true`
}
func enforceStepUp(..., settings) bool {
    if settings != nil && !settings.IsStepUpEnabled(...) { return true }
    // ...
    sessionKey, hasSession := StepUpSessionKey(c, subject.UserID)
    if !hasSession {
        AbortWithError(c, 403, "STEP_UP_REFRESH_REQUIRED", "Please refresh your session and verify again")
        return false
    }
    // ...
}
```

  The existing `GenerateTokenPair` uses `familyID = hex.EncodeToString(16 random)` as `sid`, so refreshed tokens will already be correctly scoped; the change only affects the pre-rollout long tail.

- **Confidence:** medium (fallback is by design for smooth rollout, documented as “兼容性：旧 token 退化为用户级键”).

---

### S1-7 [LOW] `Secure` flag on HttpOnly cookies (`linuxdo_*`, `oauth_pending_*`, `oauth_bind_access_token`) tracks `X-Forwarded-Proto` at best — insecure deployments behind TLS-terminating proxy without that header set correctly will downgrade to `Secure: false`

- **File:** `backend/internal/handler/auth_linuxdo_oauth.go:1012` (`isRequestHTTPS`), `1033`, `1043`, `1065`, `1086` (all cookie setters)
- **CWE:** CWE-614
- **Description:** `isRequestHTTPS(c)` returns `c.Request.TLS != nil` OR `strings.ToLower(X-Forwarded-Proto)=="https"`. If the service is behind a cloud LB that terminates TLS and forwards plain HTTP without injecting `X-Forwarded-Proto: https` (or strips it), cookies are set with `Secure: false` and would be sent over plain HTTP on later requests to `http://` origins (e.g., victim visiting `http://app.example.com` via typo). HttpOnly + SameSite=Lax mitigate theft via JS, but network-sniffing attacker on same segment could capture `linuxdo_oauth_state`, `oauth_pending_browser_session`, and especially `oauth_bind_access_token` (which carries a URL-escaped `Authorization: Bearer` JWT, valid 10 min, scoped to `/api/v1/auth/oauth`). The middleware `SessionBindingContext` correctly respects `cfg.ForwardedClientIPSettings` for IP extraction but cookie `Secure` does not share that `TrustForwardedIP` gate — it blindly trusts `X-Forwarded-Proto` if TLS is absent.
- **Vulnerable code:**

```go
func isRequestHTTPS(c *gin.Context) bool {
    if c.Request.TLS != nil { return true }
    proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")))
    return proto == "https"
}
func setCookie(c *gin.Context, name string, value string, maxAgeSec int, secure bool) {
    http.SetCookie(c.Writer, &http.Cookie{
        Name: name, Value: value, Path: linuxDoOAuthCookiePath,
        MaxAge: maxAgeSec, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
    })
}
```

- **Data flow / attack scenario:** Production behind AWS ALB with `listener: 443 → target: 80` and ALB not configured to send `X-Forwarded-Proto`. Victim `GET /api/v1/auth/oauth/linuxdo/start` over HTTPS externally; ALB forwards as `http` to Go without the header → `setCookie(..., secure:false)` → cookies with `Secure` absent. If victim later accidentally makes an `http://` request to the same host (HSTS not preloaded, or IP-based access), cookies leak on the wire to a passive network observer, who replays `oauth_bind_access_token` (or steals future OAuth `state`) to bind attacker OAuth identity to victim account within 10 min.
- **Impact:** Low — requires misconfigured proxy *and* victim http downgrade *and* short 10 min window. Modern browsers + HSTS largely cover it, but the `Secure` decision should be explicit configuration, not header-sniffing at cookie-write time.
- **Fix:** Derive `Secure` from `cfg.Server.ForceHTTPS` / `cfg.Security.CookieSecure` or from global `TrustForwardedIP` setting (reuse `ForwardedClientIPSettings.TrustForwardedIP`). If behind a trusted proxy, trust `X-Forwarded-Proto`; otherwise require `c.Request.TLS != nil`. At minimum, warn/log when `!isRequestHTTPS` on a non-health endpoint that still set cookies:

```go
func isRequestHTTPS(c *gin.Context, cfg *config.Config) bool {
    if c.Request.TLS != nil { return true }
    if cfg != nil && cfg.TrustForwardedIP() {
        proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")))
        if proto=="https" { return true }
    }
    // If TLS termination is outside, caller must set Server.ForceHTTPS or TrustForwardedIP; else fail closed
    return false // and set Secure:true anyway when ForceHTTPS, or log downgrade
}
```

  Set cookies with `Secure: true` when `ForceHTTPS` is enabled regardless of `isRequestHTTPS` probe, or make `Secure` unconditionally `true` and require `HSTS` + redirect HTTP→HTTPS at edge (preferred).

- **Confidence:** medium.

---

### S1-8 [INFO] JWT parser accepts HS384/HS512 in addition to HS256 — no forgery without secret, but violates least-privilege algorithm pinning

- **File:** `backend/internal/service/auth_service.go:1363` (`jwt.WithValidMethods([]string{HS256, HS384, HS512})`) and `:1372` (`if _, ok := token.Method.(*jwt.SigningMethodHMAC)`)
- **CWE:** CWE-327
- **Description:** Tokens are always minted with `jwt.SigningMethodHS256` (`auth_service.go:1452`), but `ValidateToken` allows HS384/HS512 as well. Since all three are HMAC with the same `cfg.JWT.Secret`, an attacker still needs the secret to forge a token; there is no `none` fallback (explicitly rejected by `SigningMethodHMAC` type-assert). Functionally not exploitable, but unnecessary algorithm surface — HS384/512 are not used and have occasionally had library edge cases. Pin to HS256 only.
- **Fix:**

```go
parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
```

- **Confidence:** high (verified generate path uses HS256 exclusively).

### S1-9 [INFO] `OptionalJWTAuth` path for Model Plaza correctly fail-closed on 401 (refresh flow still triggered); no auth bypass found — confirm as secure

- **File:** `backend/internal/server/middleware/optional_jwt_auth.go:23`
- **Description:** When `Authorization` is absent → `c.Next()` (anonymous plaza view). When present → delegates to strict `jwtAuth(strict)` which enforces `TOKEN_REVOKED`, `USER_INACTIVE`, `SESSION_BINDING_MISMATCH`. Frontend 401 triggers `refresh-token` retry via `routes/auth.go:64` (`POST /api/v1/auth/refresh` rate-limited 30/min, fail-close). Not a finding; documented to prevent future regression that would silently downgrade invalid tokens to anonymous access and leak pricing-group capabilities.
- **Confidence:** high.

### S1-10 [INFO] Handler-level IDOR checks are comprehensive and correct — verified no bypass in in-scope handlers

- **File:** `backend/internal/handler/api_key_handler.go:172` (`key.UserID != subject.UserID → 404`), `usage_handler.go:93`, `:398`, `:705` (owner checks or `VerifyOwnership`), `image_playground_handler.go:198` (`*apiKey.GroupID != req.GroupID → 403` + service `availableGroupsAndKeys`), `image_task_handler.go:223` (`ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}`), `user_handler.go:50` (`ListByUser(ctx, subject.UserID)`), `redeem_handler.go`/`subscription_handler.go` (subject-scoped).
- **Description:** Systematic review of every `GetAuthSubjectFromContext` / `GetByID` + `key.UserID` comparison shows no missing ownership check. `DashboardAPIKeysUsage` enforces `VerifyOwnership` + `max 100 IDs` cap. `Admin` handlers correctly use `adminAuth` → `GetAuthSubjectFromContext` → `AdminService` scoping, not user ID from request. The `ResolveAPIKey` playground path was examined for cross-tenant group confusion (attacker-supplied `group_id` belonging to another user) — blocked by `ImagePlaygroundService.ResolveAPIKey` which intersects `availableGroupsAndKeys(ctx, subject.UserID)` with `isImagePlaygroundGroup` and candidate key filtering.
- **Fix:** None required; retain as regression guard. If adding new `group_id` or `api_key_id` query params, always call `apiKeyService.GetByID` + owner compare or `apiKeyService.VerifyOwnership` before acting.
- **Confidence:** high. Grepped all `subject.UserID` sinks and cross-checked each handler's authorization preamble.

---

## Non-Findings / Explicitly Verified as Secure (Do Not File)

- **Admin privilege escalation / route gaps:** All `RegisterAdminRoutes` inherit `adminAuth` (JWT admin or `x-api-key` admin key with `subtle.ConstantTimeCompare`) + `AdminComplianceGuard` (block until compliance ACK, bypass only for `/admin/compliance`). No admin handler was found reachable via `jwtAuth`/`apiKeyAuth` alone. `admin_only.go:AdminOnly` compares `role == RoleAdmin` string constant, not `strings.EqualFold` — but `role` is set only from DB `users.role`, not user-controlled, so no case-confusion bypass.
- **JWT alg confusion / `none` / empty secret:** `ValidateToken` length-gates at 8192, parser restricts to HMAC family, `ParseWithClaims` callback type-asserts `*jwt.SigningMethodHMAC`, `jwt.secret` validated `>=32 bytes` in `config.go:2751` (and generated via `generateJWTSecret(32)` when missing via allowMissing flow, never empty at steady state). No bypass.
- **Session fixation:** Access tokens mint new `sid` (`hex 16 random → 32 hex` via `familyID` in `GenerateTokenPair`, or 8-byte sessionID in `GenerateToken`). No `sid` is taken from client input. Good.
- **Open redirect:** `sanitizeFrontendRedirectPath` enforces `path[0]=='/'`, `!hasPrefix("//")`, `!contains("://")`, `!contains("\r\n")`, `len<2048`. `redirectWithFragment` also checks `u.Scheme` is empty or `http/https`. Truncation at 512/2048 prevents header injection. Good.
- **OAuth CSRF / PKCE:** Every `*OAuthStart` generates `oauth.GenerateState()` (32 random, base64url, 43+ chars) + `GenerateCodeVerifier()` (32 random → 43 chars) when `UsePKCE`, stored `base64.RawURLEncoding` in HttpOnly `SameSite=Lax` cookies (10 min), compared `state != expectedState` on callback, deleted via `defer clearCookie`. No `state` leakage via Referer beyond fragment concern above.
- **CORS:** Already inventoried; no `*` with credentials, no regex bypass, no reflection of `Origin` header when `*` is configured. Good.
- **Auth header / token logging:** `Logger`/`RequestLogger` log `method, path (no query), status, latency, client_ip, protocol, account_id, platform, model` — never `Authorization`, `x-api-key`, `password`, `Token`. `audit_log` flags `ingress_reject_reason` but does not log token material.
- **Password hashing / reset:** `bcrypt.DefaultCost` (10) on `HashPassword`, `CheckPassword` via `CompareHashAndPassword`. Reset token `GeneratePasswordResetToken` = 32 random bytes → 64 hex (256-bit), `passwordResetTokenTTL=30m`, `ConsumePasswordResetToken` constant-time compare + `DEL`, email cooldown 30 s, verification codes constant-time compare, max 5 attempts per 15 min. Good.

---

## Appendix: Severity Rationale

- **Critical** reserved for unauthenticated mass breach / RCE; none found in authN/authZ scope.
- **High** = broken authentication on reachable path, session takeover without brute force, privilege escalation from user→admin, or design gap that makes another exploit trivial (S1-1/S1-2).
- **Medium** = security-relevant misconfiguration or leak that aids takeover with an extra step or misconfigured env (S1-3/S1-4/S1-5).
- **Low** = edge-case session scoping or header heuristic that is mitigated by default deployment (HSTS, short TTL).
- **INFO** = correct-by-construction patterns worth pinning (algorithm, optional-auth, IDOR-negative).
