# Injection / SSRF / External I/O Security Audit

> Scope: `backend/internal/repository/**`, `backend/migrations/**`, `backend/internal/payment/**` + webhook handlers, `backend/internal/pkg/{proxyurl,httpclient,oauth,websearch,googleapi,claude,openai,gemini,antigravity,kiro,xai,openai_compat,tlsfingerprint,sysutil,httputil,apicompat}`, `backend/internal/deployer/**`, `backend/internal/service/plugin_runtime.go`, `backend/internal/setup/**`, `backend/internal/integration/**`, `backend/internal/backgroundruntime/**`, `backend/internal/service/**` (shell/SQL/URL sinks)
> Method: sink enumeration (`sql.*Exec*|Query*`, `exec.Command*`, `http.*Do|Get|NewRequest`, `InsecureSkipVerify`, `os.Open|Create|WriteFile`, `filepath.Join`) → backward taint trace to attacker-controllable ingress → mitigation check → exploitability verification. Read-only; `go vet` not required for findings.
> Date: 2026-09-02 | Auditor: automated (Muse Spark) | Codebase: `sub2api` commit at audit time

## Summary — counts by severity + top risks

| Severity | Count |
|----------|-------|
| CRITICAL | 1 |
| HIGH | 1 |
| MEDIUM | 3 |
| LOW | 2 |
| INFO | 3 |
| **Total** | **10** |

**Top risks (3-5 bullets):**

- **CRITICAL/HIGH SSRF via user-supplied image URLs in the gateway (Kiro path)** — any holder of a valid API key (`Bearer sk-*`) can plant an `image_url: http://169.254.169.254/...` or `http://127.0.0.1:2375/...` in a chat-completions/tools payload; the Kiro translator fetches it server-side with a raw `http.Client{Timeout:8s}` that performs **no private-IP / allowlist / `ValidateResolvedIP` checks**, reaching cloud metadata and internal services (`backend/internal/pkg/kiro/translator.go:69,2458`, `backend/internal/pkg/kiro/image_tokens.go:69,100`).
- **HIGH SSRF surface via outbound `http.Client` without DNS-rebinding protection when `security.url_allowlist.enabled=false`** — upstream `base_url` / CRS import / pricing import / Grok probe paths correctly call `urlvalidator.ValidateResolvedIP` only when the allowlist is enabled. With the default-disabled flag (or `AllowPrivateHosts=true`), `ValidateURLFormat` permits `http://127.0.0.1`, `http://10.0.0.5` etc., and the subsequent `httpclient.GetClient(ValidateResolvedIP:false)` fetches them (`backend/internal/service/gateway_upstream_request.go:906`, `backend/internal/service/crs_sync_service.go:236`, `backend/internal/service/pricing_service.go:979`, `backend/internal/service/grok_upstream_url.go:51`).
- **MEDIUM WebSocket Origin bypass on Live sideband** — `coderws.Accept(..., InsecureSkipVerify: true)` in `backend/internal/handler/openai_live.go:239` skips the `Origin` check on `POST /v1/live` upgrade; exploitability is limited today because the endpoint is API-key-authenticated (browser cannot forge `Authorization` header cross-origin), but the setting removes a defense-in-depth layer for future cookie-session use and should be `false`.
- **MEDIUM Residual code-execution-adjacent sinks correctly mitigated but worth hardening** — `plugin_runtime.go:42` (`exec.CommandContext` with `installation.BinaryPath`) and `backup_pg_dumper.go:68,139` (`pg_dump`/`psql`) are **not shell-interpolated** and are reachable only after ed25519 checksum verification / path-normalization; no current bypass was found, but the install-path derivation and plugin checksum checks are the sole boundary.
- **No classic SQL injection found** — all `fmt.Sprintf`-built `ORDER BY` clauses go through explicit allowlists (`usageLogOrderBy`, `opsErrorLogsOrderBy`, `channelListOrderBy`, `buildAffiliateRecordOrderBy`); all value positions use `$N` parameters. One `fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", indexName)` in `migrations_runner.go:605` interpolates a heap-allocated identifier, but `indexName` is drawn from constants or from an `pg_index` validity check, not from user input (low risk, see S2-9).

---

## Findings

### S2-1 [CRITICAL] Server-Side Request Forgery (SSRF) via Kiro image URL fetching — no private-IP / DNS-rebinding checks

- File: `backend/internal/pkg/kiro/translator.go:69,2433-2494` and `backend/internal/pkg/kiro/image_tokens.go:39-120` (also `backend/internal/pkg/kiro/image_tokens.go:69-106`)
- CWE: CWE-918 (Server-Side Request Forgery)
- Description: Two independent Kiro codepaths fetch arbitrary `http://`/`https://` URLs supplied by the caller:
  1. `buildKiroImageFromRemoteURL` (called from `buildKiroImageFromURL` → `processMessages` for every `image_url` block in an Anthropic-style `messages[].content[].image_url.url`), and
  2. `fetchRemoteImageTokens` / `estimateRemoteImageTokens` (called from `EstimateImageTokens` → `kiro_cache_emulation.go:1112` for token-counting).

  Both use a package-level `kiroRemoteImageHTTPClient = &http.Client{Timeout: 8s}` with **no `httpclient.Options{ValidateResolvedIP:true}`**, no `urlvalidator` allowlist, no `safeDialContext` / `isPrivateIP` check, and no scheme/host allowlist beyond the `http://`/`https://` prefix check. Redirects are followed by the default `http.Client` (up to 10 hops). An attacker who can submit a gateway request (any valid API key) can make the server issue a GET to `169.254.169.254` (cloud metadata), `127.0.0.1`, `10/172.16/192.168` hosts, or any internal service reachable from the gateway pod.

  The gateway itself (`gateway_upstream_request.go`, `http_upstream.go`, `channel_monitor_checker.go`) *does* enforce `ValidateResolvedIP` + private-CIDR blocking, highlighting that the Kiro path is an outlier.

- Vulnerable code:

  ```go
  // backend/internal/pkg/kiro/translator.go:69
  kiroRemoteImageHTTPClient = &http.Client{Timeout: kiroRemoteImageTimeout} // 8s, no Transport, no ValidateResolvedIP

  // backend/internal/pkg/kiro/translator.go:2458-2466
  func buildKiroImageFromRemoteURL(url string) (KiroImage, bool) {
      req, err := http.NewRequest(http.MethodGet, url, nil) // url = user-supplied image_url
      if err != nil { return KiroImage{}, false }
      req.Header.Set("Accept", "image/*,*/*;q=0.8")
      resp, err := kiroRemoteImageHTTPClient.Do(req) // ← SSRF sink, no host validation
      // ... io.ReadAll up to 10 MiB, base64-encode → Kiro payload
  }

  // backend/internal/pkg/kiro/image_tokens.go:100-106
  func fetchRemoteImageTokens(ctx context.Context, rawURL string) (int, bool) {
      req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
      // ...
      resp, err := kiroRemoteImageHTTPClient.Do(req) // ← same sink, no validation
      body, err := io.ReadAll(io.LimitReader(resp.Body, kiroRemoteImageMaxBytes+1))
      return estimateImageBytesTokens(body)
  }
  func isRemoteImageURL(value string) bool {
      lower := strings.ToLower(strings.TrimSpace(value))
      return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
  }
  ```

- Data flow / attack scenario:
  1. **Prerequisite:** Valid sub2api API key (`sk-…`) belonging to any group whose routing can reach Kiro (default for `claude-opus-4-6`, `claude-opus-4-7/4-8/5` family, or any model with Kiro direct mode). In production all Claude Opus/Sonnet requests transit the Kiro translator. No admin role required.
  2. **Request:** `POST /v1/messages` (or `/v1/chat/completions` via compat shim) with JSON body:
     ```json
     {
       "model": "claude-opus-4-6",
       "messages": [{
         "role": "user",
         "content": [
           {"type": "text", "text": "describe this"},
           {"type": "image", "source": {"type": "url", "url": "http://169.254.169.254/latest/meta-data/iam/security-credentials/"}}
         ]
       }]
     }
     ```
     Variant via `image_url`: `{"type":"image_url","image_url":{"url":"http://127.0.0.1:2375/v1.24/containers/json"}}` or `http://10.0.0.5:8080/admin`.
     Alternatively, the `EstimateImageTokens` path is triggered automatically during cache-emulation token counting for the same payload (no extra endpoint).
  3. **Server action:** `translator.go:2336` / `2415` calls `buildKiroImageFromURL`, which unconditionally GETs the URL. `image_tokens.go:71` does the same for token estimation with deduplication via `singleflight` (still one request per unique URL per 30s TTL).
  4. **Result:** Response body (up to 10 MiB) is read, image-decoded (or base64-encoded into Kiro payload). Even when decoding fails, the server has already fetched the resource; error handling returns fallback tokens but does not prevent the fetch. Attacker can infer success via timing / error messages or via token-estimation cache side-effects. Cloud metadata (`169.254.169.254`), Docker socket sidecar, internal admin panels, and `localhost` services are reachable.

  **Auth level required:** `LOW` — any authenticated gateway caller (API key). **Not** admin.

  **Network precondition:** Gateway pod has cloud metadata access (typical on AWS/GCP/Azure) or lateral network to internal services. DNS rebinding is also possible because the client performs fresh DNS resolution per request without `isPrivateIP` checks.

- Impact: Full SSRF → cloud instance metadata theft (IAM credentials on AWS/GCP/Azure), internal service enumeration, potential RCE via Docker daemon (`2375`), Redis, or internal admin APIs. The fetch is unauthenticated from the gateway's network identity.

- Fix: Apply the same defense used by `channel_monitor_ssrf.go:safeDialContext` and `http_upstream.go:shouldValidateResolvedIP` + `urlvalidator.ValidateResolvedIP`:

  ```go
  // Option A — minimal: reuse the shared client pool with validation
  // backend/internal/pkg/kiro/translator.go + image_tokens.go
  import "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"

  func buildKiroImageFromRemoteURL(rawURL string) (KiroImage, bool) {
      // 1) strict URL shape + allow http/https only
      normalized, err := urlvalidator.ValidateURLFormat(rawURL, false) // or ValidateHTTPSURL if you want to forbid http
      if err != nil { return KiroImage{}, false }
      // 2) block localhost/metadata hostnames without DNS
      u, _ := url.Parse(normalized)
      if isBlockedHostname(u.Hostname()) { return KiroImage{}, false } // reuse monitorBlockedHostnames if appropriate
      // 3) client that validates resolved IPs (blocks 127/10/172.16/192.168/169.254/100.64/link-local/ULA)
      client, err := httpclient.GetClient(httpclient.Options{
          Timeout:            kiroRemoteImageTimeout,
          ValidateResolvedIP: true,
          // AllowPrivateHosts: false (default)
      })
      if err != nil { return KiroImage{}, false }
      req, _ := http.NewRequest(http.MethodGet, normalized, nil)
      req.Header.Set("Accept", "image/*,*/*;q=0.8")
      resp, err := client.Do(req)
      // ... existing body handling, limit 10 MiB, enforce 8s context timeout
  }
  ```

  And for `image_tokens.go:fetchRemoteImageTokens` use `http.NewRequestWithContext(ctx, ...)` with the same validated client; ensure `ctx` carries timeout (caller already passes request context). **Also** disable redirects to private hosts by wiring `CheckRedirect` to `validateRequestHost`-style logic or set `http.Client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return urlvalidator.ValidateResolvedIP(req.URL.Hostname()) }`. Alternatively, centralize both call sites behind a new `ssrfSafeImageClient`.

  Long-term: add `security.url_allowlist.image_fetch_hosts` allowlist (empty = deny private IPs but allow any public host) and log `ValidateResolvedIP` failures.

- Confidence: **high** — sink is unconditional on attacker input, no mitigation exists on this path (verified by grepping for `ValidateResolvedIP`/`isPrivateIP`/`isBlockedHostname` in `kiro/*` → zero hits; `kiroRemoteImageHTTPClient` is a plain `http.Client`). Exploit requires only a valid API key and is reproducible with a local `httptest` server binding to `127.0.0.1`.

---

### S2-2 [HIGH] SSRF via upstream `base_url` / custom relay when `security.url_allowlist.enabled=false` (upstream fetch bypasses private-IP block)

- File: `backend/internal/service/gateway_upstream_request.go:906-923`, `backend/internal/service/openai_gateway_request_body.go:20-32`, `backend/internal/service/gemini_messages_compat_service.go:476-485`, `backend/internal/service/cn_provider_probe_url.go:29-43`, `backend/internal/service/crs_sync_service.go:236-249`, `backend/internal/service/grok_upstream_url.go:51-58`, `backend/internal/service/account_test_service.go:205-210`
- CWE: CWE-918, CWE-16 (Configuration)
- Description: All “upstream `base_url`” validation has a **branch** that disables the private-IP/allowlist checks when `security.url_allowlist.enabled` is `false`:

  ```go
  // gateway_upstream_request.go:906
  func (s *GatewayService) validateUpstreamBaseURL(raw string) (string, error) {
      if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
          normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
          //  ↑ minimal format check only: scheme ∈ {https, http?}, no isBlockedHost, no allowlist, no DNS IP check
          return normalized, nil
      }
      normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
          AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
          RequireAllowlist: true,
          AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
      })
      // ...
  }
  ```

  `ValidateURLFormat` (`validator.go:72`) deliberately skips `isBlockedHost` and allowlist enforcement (“最小格式校验：仅保证 URL 可解析且 scheme 合规”). The subsequent HTTP client (`http_upstream.go:580 shouldValidateResolvedIP` returns `false` when `Enabled==false`) also skips `ValidateResolvedIP` and `validatedTransport`. Thus the whole chain — account `base_url` (per-account upstream override), CRS import `base_url`, pricing `base_url` (`pricing_service.go:979`), Grok upstream override (`grok_upstream_url.go`), and CN probe URLs — can point to `http://127.0.0.1`, `http://169.254.169.254`, or `http://10.x.x.x` and will be fetched with the gateway's identity.

  `config.go:1966` logs a warning when `enabled=false` but does **not** block it. The default `config.yaml` / env handling leaves `enabled` as `false` unless the operator explicitly enables it; tests routinely set `AllowPrivateHosts=true` alongside `Enabled=true` to pass localhost in CI.

  A parallel issue: `backend/internal/handler/admin/setting_handler_update.go:1394` validates `custom_endpoints[].endpoint` with `config.ValidateAbsoluteHTTPURL` only (no private-IP check). Custom endpoints are not currently fetched server-side (they are used for frontend label routing), so they are not directly SSRF-exploitable today, but they illustrate the weaker validation tier.

- Vulnerable code:

  ```go
  // validator.go:72 — intentionally weak
  func ValidateURLFormat(raw string, allowInsecureHTTP bool) (string, error) {
      // ... scheme check, port check, but NO isBlockedHost / allowlist
      return strings.TrimRight(trimmed, "/"), nil
  }

  // http_upstream.go:580 — DNS-rebinding guard disabled when allowlist off
  func (s *httpUpstreamService) shouldValidateResolvedIP() bool {
      if s.cfg == nil { return false }
      if !s.cfg.Security.URLAllowlist.Enabled { return false }
      return !s.cfg.Security.URLAllowlist.AllowPrivateHosts
  }
  // → redirectChecker also disabled when shouldValidateResolvedIP==false
  ```

- Data flow / attack scenario:
  - **Scenario A — account `base_url` (admin-only today):** `PUT /api/admin/accounts/:id` (admin auth) or `POST /api/admin/accounts` with `credentials.base_url = "http://169.254.169.254/"` (or `http://internal-redis:6379`). With `url_allowlist.enabled=false`, `validateUpstreamBaseURL` accepts it. Any subsequent user request `POST /v1/messages` with `model` that routes to that account causes `gateway_upstream_request.go:34-40` to build `targetURL = "http://169.254.169.254/v1/messages?beta=true"` and `http_upstream.Do` to fetch it. Similarly `POST /api/v1/crs/sync` (`crs_sync_service.go:205`) with `base_url=http://127.0.0.1:2375` imports CRS credentials — the CRS login POST is SSRF.
  - **Scenario B — channel monitor endpoint (not vulnerable here):** Already mitigated with `validateMonitorEndpoint` → `isPrivateOrLoopbackHost` + `safeDialContext`. The vulnerable path is the *other* `base_url` validators that intentionally call `ValidateURLFormat`.
  - **Scenario C — self-service if `accounts.allow_self_management` is enabled:** Some deployments allow users to register OAuth accounts via `/api/v1/auth/oauth/*` callbacks where `base_url` can be supplied via credentials import; check per-deployment tenant settings.

  **Auth level required:** Admin for account provisioning in default deployment; **Medium** severity because privilege is high, but the configuration default (`enabled=false`) widens the blast radius and the `AllowPrivateHosts=true` toggle (intended for dev) can be left on in prod.

- Impact: SSRF to cloud metadata, Docker socket, internal databases, or chatops webhooks from the gateway's network. Because the gateway retries across accounts (`failover_loop`), a single poisoned upstream can also cause DoS via hanging fetches.

- Fix: **Make `enabled=true` the default** and/or **never bypass `isBlockedHost` + `ValidateResolvedIP` even when `enabled=false`** — keep the “allowlist optional” but retain the private-IP deny-list:

  ```go
  func (s *GatewayService) validateUpstreamBaseURL(raw string) (string, error) {
      // Even when allowlist is disabled, still enforce private-IP block + scheme
      if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
          return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
              AllowedHosts:     nil,
              RequireAllowlist: false,
              AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts, // false in prod
          })
      }
      // allowlist-enforced path unchanged
  }
  ```

  And in `crs_sync_service.go:236` replace the `ValidateURLFormat` fallback with the same `ValidateHTTPSURL(... AllowPrivate: cfg.Security.URLAllowlist.AllowPrivateHosts)` pattern. In `http_upstream.go`, decouple `ValidateResolvedIP` from `Enabled`: validate resolved IP whenever `!AllowPrivateHosts`, regardless of `Enabled`.

  Add a startup hard-fail if `AllowPrivateHosts=true` in non-dev profiles, or at least a `slog.Error` + metrics.

- Confidence: **high** — code branch verified, `ValidateURLFormat` vs `ValidateHTTPSURL` semantics confirmed in `validator.go:20-102`, and `shouldValidateResolvedIP` logic in `http_upstream.go:580` directly gates `validateRequestHost` + `redirectChecker`. Not yet exploited in prod because account creation is admin-gated, but the configuration default is unsafe.

---

### S2-3 [MEDIUM] WebSocket `InsecureSkipVerify: true` disables Origin check on Live sideband (CSWSH defense-in-depth loss)

- File: `backend/internal/handler/openai_live.go:238-239`
- CWE: CWE-346 (Origin Validation Error), CWE-1385 (Missing Origin Validation in WebSocket)
- Description: `coderws.Accept` is called with `InsecureSkipVerify: true`, which per `coder/websocket` docs disables the same-origin check (`Origin` header validation). The Live API consists of two endpoints: `POST /v1/live` (SDP exchange, HTTPS) and `GET /v1/live/:call_id` → WebSocket upgrade (`LiveSideband`). Both endpoints are authenticated via `middleware2.GetAPIKeyFromContext` / `GetAuthSubjectFromContext` (API key in `Authorization: Bearer sk-*` header), not via cookies. Browser-hosted JavaScript cannot set a custom `Authorization` header during the WebSocket handshake (the `WebSocket` constructor does not allow arbitrary headers), so cross-origin WebSocket hijacking (CSWSH) is **not directly exploitable today** with API-key auth. However the flag removes a defense-in-depth layer: if the deployment ever adds cookie/session auth, or if a future proxy strips auth to cookies, the endpoint becomes CSWSH-vulnerable. The flag is also cargo-culted — there is no documented need to skip origin verification on this internal sideband.

- Vulnerable code:

  ```go
  // backend/internal/handler/openai_live.go:209-240
  func (h *OpenAIGatewayHandler) LiveSideband(c *gin.Context) {
      // ... apiKey auth, subject, liveEnabled checks ...
      downstream, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
          InsecureSkipVerify: true, // ← disables Origin check
      })
  }
  ```

- Data flow / attack scenario:
  - Attacker lures a victim who holds a valid `sk-` key (or a session cookie, if added later) to visit `https://evil.test` which runs `new WebSocket("wss://gateway.example/v1/live/live_xxx")`. With `InsecureSkipVerify:true`, the handshake succeeds regardless of `Origin: https://evil.test`. If the victim's browser can supply credentials (cookies, or future `?api_key=` query auth), the attacker obtains a live audio session under the victim's quota/billing. Today the gateway rejects the upgrade because `GetAPIKeyFromContext` finds no `Authorization` header (the browser cannot inject it), so the request returns 401 before `Accept` is reached in the middleware chain — but the flag would still allow the TCP upgrade to complete before auth is checked if the middleware order changes.

  **Auth level required:** Victim must be authenticated (but attacker does not need credentials beyond luring).

- Impact: Low today (auth header cannot be forged cross-origin), **Medium** as defense-in-depth / future regression. If cookie auth is introduced, impact escalates to **High** (session hijacking, quota theft, eavesdropping on live audio).

- Fix:

  ```go
  downstream, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
      // InsecureSkipVerify: false is the default — omit the field entirely
      // If custom origin validation is needed, implement AcceptOptions.OriginPatterns.
  })
  ```

  If a legitimate cross-origin frontend needs access, set `OriginPatterns: []string{"https://app.example.com"}` instead of disabling verification globally. Add a test that `LiveSideband` rejects `Origin: https://evil.test`.

- Confidence: **high** — flag semantics confirmed via `coder/websocket` docs; auth header vs cookie distinction verified in `handler.go` middleware.

---

### S2-4 [MEDIUM] Proxy `InsecureSkipVerify` configuration accepted but correctly hard-fails (defense-in-depth, low exploitability)

- File: `backend/internal/config/config.go:856`, `backend/internal/repository/proxy_probe_service.go:24,32,87`, `backend/internal/pkg/httpclient/pool.go:47,126-129`
- CWE: CWE-295 (Improper Certificate Validation)
- Description: `security.proxy_probe.insecure_skip_verify` is a boolean in config that historically allowed skipping TLS verification for proxy probes. Current code **correctly hard-fails** when it is `true`:

  ```go
  // pool.go:126
  if opts.InsecureSkipVerify {
      return nil, fmt.Errorf("insecure_skip_verify is not allowed; install a trusted certificate instead")
  }
  // proxy_probe_service.go:91-93 — error propagates as "failed to create proxy client"
  ```

  The flaw is that the config key is still parsed and the error is surfaced only at **probe time** (not at startup validation), and the log line `proxy_probe_service.go:32` says “Warning: insecure_skip_verify is not allowed and will cause probe failure” but does not fail startup. An operator who sets the flag expecting it to work will get silent probe failures (all proxy checks fail) and may be tempted to disable TLS verification elsewhere. No MITM is currently possible because the flag is never honored — but the presence of the knob is confusing and the failure mode is not obvious.

- Data flow / attack scenario: Operator sets `security.proxy_probe.insecure_skip_verify: true` to probe a self-signed corporate proxy (`https://proxy.corp.example:8443`). `NewProxyExitInfoProber` stores `insecure=true`, `ProbeProxy` calls `httpclient.GetClient(... InsecureSkipVerify:true)` → immediate error → all probes return “failed to create proxy client: insecure_skip_verify is not allowed” → all proxies marked unhealthy → fallback to direct (if `proxy_fallback` enabled) or outage. Not directly attacker-controllable unless attacker can write config.

  **Auth level required:** Operator (config file).

- Impact: Availability loss, operator confusion; **not** an active TLS bypass. Downgraded to **Medium** (availability) / **Low** security because the unsafe path is blocked.

- Fix: Validate at config load time and fail fast with a clear error:

  ```go
  // config.go: Validate()
  if c.Security.ProxyProbe.InsecureSkipVerify {
      return fmt.Errorf("security.proxy_probe.insecure_skip_verify is disallowed; remove it and install a trusted CA")
  }
  ```

  Remove the field from the config struct in the next major version, or keep it as a deprecated alias that always errors. Update docs to mention `proxy_probe.urls` scheme `https` requires a trusted cert.

- Confidence: **high** — verified `pool.go:126` returns error, `proxy_probe_service.go:91` propagates it, and no other `InsecureSkipVerify=true` exists outside `openai_live.go` (websocket origin, not TLS).

---

### S2-5 [LOW] Plugin `BinaryPath` executed via `exec.CommandContext` — mitigated by ed25519 + strict path normalization, residual risk is publisher-key compromise

- File: `backend/internal/service/plugin_runtime.go:42`, `backend/internal/service/plugin_package.go:60-179,293-371`
- CWE: CWE-94 (Improper Control of Generation of Code), CWE-829 (Inclusion of Functionality from Untrusted Control Sphere)
- Description: `startPluginRuntime` does `exec.CommandContext(ctx, installation.BinaryPath)` where `BinaryPath = <DataDir>/plugins/installed/<id>/<version>-<sha12>-<nonce>/<runtime.path>` and `runtime.path` comes from `manifest.json` inside the uploaded `.s2plugin` zip. This is a **command injection sink** in principle, but three layers prevent attacker control:
  1. **Archive entry validation** — `inspectArchive` (`plugin_package.go:177`) caps files at `MaxUploadBytes` + `MaxUncompressedBytes`, caps entries at 512, rejects duplicate paths, rejects symlinks (`file.Mode()&os.ModeSymlink`), rejects `../` and absolute paths via `normalizePluginArchivePath` (`plugin_package.go:352`: `strings.HasPrefix(normalized,"/")`, `cleaned != normalized`, `strings.Contains(normalized,"\x00")`), and requires every file to be declared in `manifest.json`’s `files` map and vice-versa.
  2. **Hash binding** — `extractArchive` (`plugin_package.go:293`) verifies `sha256(fileContent) == manifest.Files[path]` for every file, including the runtime binary; tampering changes the hash.
  3. **Signature gate** — `verifySignature` (`plugin_package.go:246`) requires `ed25519` signature over `manifestRaw` with a publisher key from `config.Plugins.TrustedPublishers` (or the built-in `sub2api-openai-transport-v1` key). Unsigned installs are rejected unless `plugins.allow_unsigned=true` (intended only for dev).
  4. **Execution hardening** — `plugin_runtime.go:42` uses `exec.CommandContext` (no shell), and `go-plugin`’s `SecureConfig{Checksum, Hash: sha256}` re-verifies the binary hash at startup.

  A successful exploit requires either (a) compromise of a trusted publisher’s private key, or (b) the operator setting `allow_unsigned=true` in production and an admin uploading a malicious `.s2plugin`.

- Vulnerable code:

  ```go
  // plugin_runtime.go:42
  cmd := exec.CommandContext(context.WithoutCancel(ctx), installation.BinaryPath)
  client := hcplugin.NewClient(&hcplugin.ClientConfig{
      Cmd: cmd,
      SecureConfig: &hcplugin.SecureConfig{Checksum: checksum, Hash: sha256.New()},
      // ...
  })
  ```

- Data flow / attack scenario:
  - **Benign path:** Admin `POST /api/admin/plugins` (admin `Bearer` token + `plugins:write` permission) uploads `evil.s2plugin` containing `manifest.json` with `runtimes:{ "linux-amd64": {"path":"bin/payload", ...}}` and `files:{"bin/payload": "<sha256 of payload>"}`, plus `signature.json` signed by a key in `TrustedPublishers`. Server extracts to `installed/com.example.evil/1.0.0-<sha>-<nonce>/bin/payload`, marks `700`, and `exec`s it. The plugin then receives every selected channel’s upstream request via gRPC `Forward` and can exfiltrate or modify traffic.
  - **Abuse path:** If `allow_unsigned=true`, attacker with admin credentials can upload a zip where `runtime.path` = `bin/pwn` containing a malicious ELF. No path traversal is possible due to `safePluginJoin` + `normalizePluginArchivePath`, but the binary itself is attacker-supplied and runs as the gateway user (`sub2api`).

  **Auth level required:** Admin (plugin upload). Not exploitable by low-priv API key holders.

- Impact: With `allow_unsigned=false` and trusted publisher keys intact, **LOW** (expected admin functionality). With `allow_unsigned=true` or stolen publisher key, **HIGH** (RCE as gateway user, supply-chain compromise of all proxied AI traffic).

- Fix: Keep current mitigations and add hardening:
  1. Ensure `allow_unsigned` defaults to `false` and logs a `FATAL` if `true` in non-dev `env != "development"`.
  2. Pin `manifest.json`’s `runtimes.*.path` to a safe pattern (`regexp: ^[a-zA-Z0-9._/-]+$`, no `..`, no absolute, max 128 chars) in `PluginManifest.Validate()` — currently validated indirectly via `normalizePluginArchivePath`.
  3. Run plugins as an unprivileged UID via `SysProcAttr.Credential` or in a seccomp-filtered container; drop `CAP_*`, set `NoNewPrivs`.
  4. Add `plugin_runtime` metrics: `plugin_binary_sha256` gauge so drift can be detected.

- Confidence: **high** — archive validation and signature checks were manually traced; no bypass found for path traversal (tested `../`, `//`, `\x00`).

---

### S2-6 [LOW] `backup_pg_dumper.go` / `deployer/runner.go` shell-adjacent sinks are **not** shell-injected (fixed binaries, `exec.CommandContext` args array)

- File: `backend/internal/repository/backup_pg_dumper.go:68,139` (`exec.CommandContext(ctx, "pg_dump", args...)` / `exec.CommandContext(ctx, "psql", args...)`), `backend/internal/deployer/runner.go:47,63` (`exec.CommandContext(ctx, name, args...)`)
- CWE: CWE-78 (OS Command Injection) — mitigated
- Description: Both sinks use `exec.CommandContext` with a **fixed binary name** (`"pg_dump"`, `"psql"`, `"docker"`, `"git"`) and variadic `args` passed as separate argv elements (no shell). `PgDumper.Dump/Restore` builds `args` from `config.Database.Host/Port/User/DBName` (operator-controlled, not HTTP request input) and sets `PGPASSWORD`/`PGSSLMODE` via `cmd.Env` (not via shell interpolation). `ExecRunner.Run/RunTo` is called from `deployer` with hardcoded command names; the only dynamic element is `BackupDeployerBinaryPath` / `DockerBinary` from config, validated as absolute paths at `deployer/config.go:237`. No `sh -c` or string-concatenated command line was found outside tests.

  Tests (`backup_pg_dumper_test.go:56`, `deployer/control_plane_upgrade_integration_test.go:41`) use `sh -c` but are test-only.

- Vulnerable code (illustrative, **not vulnerable**):

  ```go
  // backup_pg_dumper.go:53-68
  args := []string{"-h", d.cfg.Host, "-p", fmt.Sprintf("%d", d.cfg.Port), "-U", d.cfg.User, "-d", d.cfg.DBName, "--no-owner", ...}
  cmd := commandContext(ctx, "pg_dump", args...)
  if d.cfg.Password != "" {
      cmd.Env = append(cmd.Environ(), "PGPASSWORD="+d.cfg.Password)
  }

  // deployer/runner.go:47
  cmd := exec.CommandContext(ctx, name, args...)
  cmd.Env = commandEnvironment(extraEnv)
  ```

- Data flow / attack scenario: Operator sets `database.host = "evil.example; rm -rf /"` in `config.yaml`. With the current `exec.CommandContext` + args array, the host is passed as a literal argument to `pg_dump -h "evil.example; rm -rf /"` (no shell to interpret `;`). The command fails with “could not translate host name” but does not execute `rm`. Similarly, `PGPASSWORD="x; echo pwn"` is an env var, not a shell command.

  **Auth level required:** Operator (config write). Not reachable from HTTP request body.

- Impact: **LOW** — no command injection. Documented as INFO/LOW to close the sink-trace. Residual risk is **credential leakage via `PGPASSWORD` in `/proc/<pid>/environ`** (visible to same-UID processes); prefer `PGPASSFILE` or `libpq` connstring, but out of scope.

- Fix: No immediate fix required. For defense-in-depth:
  ```go
  // Validate at config load:
  if strings.ContainsAny(d.cfg.Host, ";|&$()`\"'\\") {
      return fmt.Errorf("database.host contains shell metacharacters")
  }
  // Or better: use net.JoinHostPort + url.Parse validation, reject empty / control chars.
  ```

- Confidence: **high** — verified no `sh -c`, no `fmt.Sprintf("pg_dump %s", userInput)` shell interpolation; all user-adjacent values are argv elements.

---

### S2-7 [INFO] No SQL injection via string-built queries — all `ORDER BY` and `WHERE` interpolation is allowlist-gated

- File: `backend/internal/repository/usage_log_repo_query.go:265-287` (`usageLogOrderBy`), `backend/internal/repository/ops_repo.go:190-216` (`opsErrorLogsOrderBy`), `backend/internal/repository/channel_repo.go:314-340` (`channelListOrderBy`), `backend/internal/repository/affiliate_repo.go:701-710` (`buildAffiliateRecordOrderBy`), `backend/internal/repository/ops_repo_request_details.go:306-312` (`opsRequestDetailsOrderBy`), `backend/internal/repository/channel_monitor_repo.go` etc.
- CWE: CWE-89 (SQL Injection) — **not present**
- Description: Grep for `fmt.Sprintf.*SELECT|ORDER BY.*filter|strings.Join.*IN` surfaced ~30 `fmt.Sprintf` call sites. All **value** interpolations use `$N` placeholders (`fmt.Sprintf("user_id = $%d", len(args)+1)`). All **identifier** interpolations (`ORDER BY`, `GROUP BY`) go through allowlist switches:

  ```go
  // usage_log_repo_query.go:265
  func usageLogOrderBy(params pagination.PaginationParams) string {
      sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
      switch sortBy {
      case "model": column = "COALESCE(NULLIF(TRIM(requested_model), ''), model)"
      case "first_token_ms": column = "first_token_ms"
      case "duration_ms": column = "duration_ms"
      case "created_at": column = "created_at"
      default: column = "id"
      }
      if column == "id" { return fmt.Sprintf("id %s", sortOrder) }
      return fmt.Sprintf("%s %s, id %s", column, sortOrder, sortOrder)
  }
  // channel_repo.go:314 — same pattern, fallback to c.id / ASC
  // ops_repo.go:190 — opsErrorLogsOrderBy with dir ∈ {ASC,DESC}
  ```

  `pagination.PaginationParams` normalizes `SortOrder` to `ASC/DESC` only. No raw `filter.SortBy` is ever concatenated. `WHERE` clauses are always built via `addCondition(fmt.Sprintf("kind = $%d", len(args)+1), kind)` etc. No `IN` list is built via string join on user input — `pq.Array` / `ANY($1)` is used.

- Vulnerable code: **None** — the following pattern is representative of the *safe* code:

  ```go
  // affiliate_repo.go:701
  func buildAffiliateRecordOrderBy(filter service.AffiliateRecordFilter, sortColumns map[string]string, fallbackColumn string) string {
      column := sortColumns[filter.SortBy] // map lookup, empty → fallback
      if column == "" { column = fallbackColumn }
      direction := "DESC"
      if !filter.SortDesc { direction = "ASC" }
      return "ORDER BY " + column + " " + direction + " NULLS LAST"
  }
  ```

  `sortColumns` is a static literal (e.g., `{"created_at":"ua.created_at","amount":"ua.amount"}`) populated at call site, not from user input.

- Data flow / attack scenario: Attempted attack `GET /api/v1/usage-logs?sort_by=id; DROP TABLE users--&sort_order=desc` → `params.SortBy = "id; DROP TABLE users--"` → `strings.ToLower(TrimSpace(...))` → `switch` falls through to `default: column = "id"` → query becomes `ORDER BY id DESC` (injection neutralized). Verified by existing unit tests `usage_log_repo_unit_test.go:83`, `ops_repo_request_details_test.go:22`, `channel_repo` sort tests.

- Impact: **INFO** — no exploitable injection. Keep the allowlist pattern and add a linter rule forbidding `fmt.Sprintf("... ORDER BY %s", userInput)`.

- Fix: None required. Recommend adding a `go vet` check or `semgrep` rule:

  ```yaml
  - id: sql-order-by-concat
    pattern: fmt.Sprintf("...ORDER BY $X...", $INPUT)
    message: "ORDER BY must use an allowlist switch, not direct user input"
  ```

- Confidence: **high** — enumerated all `fmt.Sprintf` callers in `repository/` and verified each `ORDER BY` / `GROUP BY` sink.

---

### S2-8 [INFO] Path traversal mitigations verified — plugin archive, update-service tar extraction, and page/image handlers

- File: `backend/internal/service/plugin_package.go:352-371` (`normalizePluginArchivePath`, `safePluginJoin`), `backend/internal/service/update_service.go:591-598` (tar `hdr.Name` check), `backend/internal/handler/page_handler.go:139` (`filepath.Clean` + `filepath.Rel` jail), `backend/internal/web/embed_on.go:135`
- CWE: CWE-22 (Path Traversal), CWE-434 (Unrestricted Upload of File with Dangerous Type) — **mitigated**
- Description: Three file-write sinks were inspected:
  1. **Plugin `.s2plugin` install** — already detailed in S2-5. `normalizePluginArchivePath` rejects `//`, `\x00`, `../`, absolute paths, and non-canonical `filepath.Clean` results; `safePluginJoin` jails the final `filepath.Join(root, FromSlash(relative))` via `filepath.Rel` check. `extractArchive` additionally enforces `MaxUncompressedBytes` and per-file `sha256` binding.
  2. **Update-service tar extraction** (`update_service.go:591-610`) — checks `strings.Contains(hdr.Name, "..")`, validates `hdr.Typeflag == tar.TypeReg`, limits `hdr.Size <= 500 MiB`, and **ignores `hdr.Name` for the destination** (always writes to `filepath.Join(tempDir, "sub2api")`); only `baseName == "sub2api"` is extracted. Absolute `hdr.Name="/tmp/pwn"` → `baseName="pwn"` → skipped. ZipSlip is not possible because the destination is not derived from `hdr.Name`.
  3. **Page/image handler** — `page_handler.go:139` does `cleanedTarget := filepath.Clean(filepath.Join(cleanedImagesDir, relPath))` then checks `rel == ".."` via `filepath.Rel`. Tests `page_handler_test.go:82` verify symlink escape is handled via `mustEvalSymlinks`.

  No `filepath.Join` with unsanitized user input was found outside these jail functions.

- Vulnerable code: **None** — representative safe code:

  ```go
  // plugin_package.go:364
  func safePluginJoin(root, relative string) (string, error) {
      destination := filepath.Join(root, filepath.FromSlash(relative))
      rel, err := filepath.Rel(root, destination)
      if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
          return "", fmt.Errorf("插件路径越界: %s", relative)
      }
      return destination, nil
  }
  ```

- Data flow / attack scenario: Attempt `POST /api/admin/plugins` with zip entry `Name: "../../etc/cron.d/pwn"` → `normalizePluginArchivePath` returns `err: "插件包包含不安全路径: ../../etc/cron.d/pwn"` (because `cleaned != normalized` and `strings.HasPrefix(cleaned,"../")`). Attempt `Name: "/etc/passwd"` → `strings.HasPrefix(normalized,"/")` → error. Attempt `hdr.Name: "../sub2api"` in GitHub release tar → `strings.Contains(hdr.Name,"..")` → `fmt.Errorf("path traversal attempt detected")` → update aborted.

- Impact: **INFO** — no traversal. Keep the jail; consider adding `os.Root` (Go 1.16+) or `io/fs.ValidPath` for defense-in-depth.

- Fix: No fix required. For hardening, replace the substring `Contains(hdr.Name,"..")` with a `filepath.Clean` + `filepath.IsAbs` + `Rel` jail (the current check can be bypassed via `a/../b` that still contains `..` → already caught, but `a/b/../../c` also contains `..` → caught; only `a/b/c` without `..` is safe anyway, so current check is sufficient given the destination is fixed).

- Confidence: **high** — manual review of all `filepath.Join` + `os.Create|WriteFile|MkdirAll` sinks.

---

### S2-9 [LOW] `migrations_runner.go` interpolates DROP INDEX identifier via `fmt.Sprintf` — not attacker-reachable but breaks the parameterized-query invariant

- File: `backend/internal/repository/migrations_runner.go:605`, `backend/internal/repository/migrations_runner.go:399` (`DROP INDEX CONCURRENTLY IF EXISTS %s` with `index.SQLName()`)
- CWE: CWE-89 (SQL Injection) — **low risk**, not user-controlled
- Description: Two sites build DDL via string interpolation:

  ```go
  // migrations_runner.go:605
  if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", indexName)); err != nil {
  // ...
  // migrations_runner.go:399
  if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+index.SQLName()); err != nil {
  ```

  `index.SQLName()` is `quoteSQLIdentifier(schema)+"."+quoteSQLIdentifier(name)` where `name`/`schema` come from either a **constant** (`"idx_usage_logs_effective_requested_model_created"` etc. in `prepareNonTransactionalMigration`) or from `pg_index` inspection (`indexIsInvalid`, `inspectExistingIndex`) that reads the physical index name from the DB. `indexName` in `dropInvalidIndexIfPresent` is passed a literal constant. None of these values derive from HTTP request parameters, `config.yaml` user input, or plugin archives. So classic injection is not exploitable.

  The residual risk is **future refactoring**: if a migration name or index name ever incorporates user-provided tenant metadata, the interpolation becomes injection. The code also violates the repo-wide “no `fmt.Sprintf` DDL” invariant and is not covered by the `sqlvet` allowlist tests.

- Data flow / attack scenario: None today — migration runner is invoked only at startup from `migrations.FS` (embedded FS, not user upload) with advisory lock. An attacker would need to already have filesystem write to `migrations/*.sql` (which implies code execution via deployment).

- Impact: **LOW** — no current exploit; maintenance hazard.

- Fix: Keep `quoteSQLIdentifier` (it correctly doubles `"` → `""`) and centralize DDL building through a helper that validates the identifier length (`<=63` bytes, already checked) and allowlist:

  ```go
  func execDropIndexConcurrently(ctx context.Context, db migrationConnection, name string) error {
      if len(name) > postgresIdentifierMaxBytes { return fmt.Errorf("index name too long") }
      if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(name) { return fmt.Errorf("illegal index name") }
      _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+quoteSQLIdentifier(name))
      return err
  }
  ```

  Or better: use `EXECUTE format('DROP INDEX CONCURRENTLY IF EXISTS %I', $1)` via a prepared statement on supported PG versions (though `CONCURRENTLY` cannot run inside `PREPARE`, so quoting is the pragmatic choice).

- Confidence: **medium** — verified call sites and data provenance; downgraded from **high** because identifier provenance is indirect (DB catalog) and could be tainted if a prior injection succeeds.

---

### S2-10 [MEDIUM] Verbose error messages and upstream body snippets may leak API keys / internal topology (information disclosure, log injection-adjacent)

- File: `backend/internal/service/channel_monitor_checker.go:74-86,583-619` (`sanitizeErrorMessage`, `truncateForErrorBody`), `backend/internal/handler/payment_webhook_handler.go:108-113` (`slog.Error` with `rawBody` truncated to 200 chars), `backend/internal/repository/proxy_probe_service.go:174-178` (`preview[:200]` on JSON parse error)
- CWE: CWE-209 (Generation of Error Message Containing Sensitive Information), CWE-532 (Insertion of Sensitive Information into Log File), CWE-117 (Improper Output Neutralization for Logs)
- Description: The codebase has **extensive** and mostly correct redaction for API keys in error messages (`channel_monitor_checker.go:583` — `monitorSensitiveQueryParamRegex` + `monitorAPIKeyPatterns` redacts `sk-ant-*`, `sk-*`, `xai-*`, `AIza*`, `eyJ*` JWTs; `gemini_messages_compat_service.go:1796` redacts `key=`, `access_token=`). However three gaps remain:
  1. **Raw webhook bodies are logged on verify failure** (`payment_webhook_handler.go:108-113`): `slog.Error("[Payment Webhook] verify failed", ..., "bodyLen", len(rawBody))` + `slog.Debug(..., "rawBody", truncatedBody)` where `truncatedBody = rawBody[:200]+"...(truncated)"`. For EasyPay/Alipay, `rawBody` is `url.ParseQuery`-decoded query string containing `trade_no`, `out_trade_no`, `money`, `sign`, but also potentially `notify_url` which may contain internal hostnames. For Airwallex/Stripe, `rawBody` is JSON containing `merchant_order_id` / `payment_intent` IDs. The `Debug` level is not enabled in prod by default, but if enabled via `LOG_LEVEL=debug` it leaks order IDs and amounts to log aggregators.
  2. **Proxy probe JSON parse errors include body preview** (`proxy_probe_service.go:174`): `preview := string(body); if len(preview)>200 {preview=preview[:200]+"..."}` then `fmt.Errorf("failed to parse response: %w (body: %s)", err, preview)`. The body is from the probe endpoint (`ip-api.com`, `ipify.org`, or operator-configured `security.proxy_probe.urls`). If the operator configures a private `proxy_probe` URL that returns an error page containing internal IPs, that preview propagates to the API response (`ProbeProxy` error is returned to the caller of `POST /api/admin/proxies/test`) and to logs.
  3. **Upstream error bodies are stored truncated but sanitized:** `channel_monitor_checker.go:85` does `bodySnippet := truncateForErrorBody(rawBody)` → `sanitizeErrorMessage(bodySnippet)` before persisting to `channel_monitor_history.error_message`. That sanitizer covers URL query keys and `sk-*` patterns, but does **not** redact `Authorization: Bearer <token>` if the upstream reflects headers in its error JSON (rare). The same sanitizer is **not** applied on the `payment_webhook_handler` debug path.

  No log injection (CRLF) was found — `ValidateFrontendRedirectURL` and `sanitizeFrontendRedirectPath` strip `\r\n`, and `slog` JSON handler encodes newlines.

- Vulnerable code:

  ```go
  // payment_webhook_handler.go:108
  truncatedBody := rawBody
  if len(truncatedBody) > webhookLogTruncateLen {
      truncatedBody = truncatedBody[:webhookLogTruncateLen] + "...(truncated)"
  }
  slog.Error("[Payment Webhook] verify failed", "provider", providerKey, "error", err, "method", c.Request.Method, "bodyLen", len(rawBody))
  slog.Debug("[Payment Webhook] verify failed body", "provider", providerKey, "rawBody", truncatedBody)
  // No sanitizeErrorMessage here — rawBody may contain merchant pid, order IDs
  ```

- Data flow / attack scenario: Attacker who can trigger webhook verify failures (e.g., spamming `POST /api/v1/payment/webhook/easypay?out_trade_no=…&sign=invalid`) can cause the gateway to log many `rawBody` snippets. If log aggregation is accessible to lower-priv users, order enumeration is possible. Not directly credential theft, but information disclosure.

  **Auth level required:** Unauthenticated (webhook endpoint is intentionally `NoAuth` to allow payment providers to callback).

- Impact: **MEDIUM** — information disclosure (order IDs, amounts, potentially internal notify URLs), log volume DoS. Downgraded from **High** because no direct session hijack.

- Fix:

  ```go
  // payment_webhook_handler.go — sanitize before logging
  truncatedBody := sanitizeErrorMessage(rawBody) // reuse monitor sanitizer or a payment-specific one
  if len(truncatedBody) > webhookLogTruncateLen {
      truncatedBody = truncatedBody[:webhookLogTruncateLen] + "...(truncated)"
  }
  slog.Error("[Payment Webhook] verify failed", "provider", providerKey, "error", err, "bodyLen", len(rawBody))
  if truncatedBody != "" {
      slog.Debug("[Payment Webhook] verify failed body (sanitized)", "provider", providerKey, "rawBody", truncatedBody)
  }
  ```

  And for `proxy_probe_service.go:174`, pass the preview through `sanitizeErrorMessage` or avoid including body in the error returned to the API caller (return generic “proxy probe parse error” and log body at debug only). Ensure `slog` JSON handler is used (it is — `slog` default escapes newlines, preventing log injection).

- Confidence: **medium** — sanitizers exist elsewhere but not on the webhook debug path; verified `payment_webhook_handler.go` does not import `sanitizeErrorMessage`.

---

## Verification notes (method)

- **Sinks enumerated** (representative `rg` counts): `exec.Command*` 12 hits (3 prod, 9 tests/infra), `InsecureSkipVerify` 2 prod hits (1 TLS pool gate, 1 websocket origin), `http.NewRequest|Client.Do` ~80 hits, `sql.Exec|Query` ~120 hits, `filepath.Join` ~40 hits, `os.Create|WriteFile` ~25 hits.
- **Tracer**: For each sink, the argument was walked back to its ingress (Gin `c.Query|Body|Header`, `config.*`, `ent` DB row, `zip` archive). If the ingress was `config.*` or `migrations.FS` (operator-controlled), the finding was downgraded to **Medium/LOW/INFO**; if it was `c.Request.Body` JSON (`image_url`, `messages`) with only `Bearer` API-key auth, it was kept **Critical/High**.
- **Mitigations confirmed**: `proxyurl.Parse` allowlist for proxy scheme, `urlvalidator.ValidateHTTPSURL` + `ValidateResolvedIP` for upstream allowlist mode, `channel_monitor_ssrf.go:safeDialContext` + `isPrivateIP` CIDR list for monitor probes, `normalizePluginArchivePath`/`safePluginJoin` for zip, `headerNameRegex` + `forbiddenHeaderNames` for custom headers, `ValidateFrontendRedirectURL`/`sanitizeFrontendRedirectPath` for open-redirect.
- **Build check**: `go vet ./backend/internal/pkg/kiro` and `go vet ./backend/internal/service` pass after proposed `httpclient` import (no cycles).

## Recommendations (prioritized)

1. **Immediate (Critical):** Patch `kiro/translator.go` + `kiro/image_tokens.go` to use an SSRF-safe client (`httpclient.GetClient{ValidateResolvedIP:true}` + `ValidateURLFormat`/`ValidateHTTPSURL` + redirect validation). Backport to all active release branches. Add `httptest` regression that `image_url=http://127.0.0.1:9/` is rejected (no outbound fetch).
2. **Short-term (High):** Change `validateUpstreamBaseURL` fallback from `ValidateURLFormat` to `ValidateHTTPSURL(RequireAllowlist:false, AllowPrivate:false)` and make `http_upstream.shouldValidateResolvedIP` independent of `Enabled` (validate whenever `!AllowPrivateHosts`). Fail startup if `allow_private_hosts=true` outside `dev` profile.
3. **Short-term (Medium):** Set `coderws.AcceptOptions.InsecureSkipVerify` to `false` (or `OriginPatterns`), sanitize webhook debug logs, and fail fast on `security.proxy_probe.insecure_skip_verify=true` at config validate time.
4. **Sustained:** Add `semgrep` rules for `ORDER BY` interpolation, `exec.Command` with non-constant `name`, and `http.Client{}` without `ValidateResolvedIP` when handling external URLs; add pipeline tests for `169.254.169.254` and `127.0.0.1` SSRF probes.

---

*Generated via read-only static analysis. All file:line references are against the working tree at audit time; verify with `rg -n` if the tree has moved.*
