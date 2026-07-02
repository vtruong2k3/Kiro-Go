# Design Document

## Overview

This feature adds a seventh account login method — **"Enterprise SSO - Microsoft 365"** (the `MS365_Method`) — to the Kiro-Go admin panel's "Add Account" dialog. The method registers a Microsoft 365 / Microsoft Entra ID (Azure AD) tenant account so it can join the multi-account pool and serve proxy requests exactly like the six existing methods.

### Design-phase investigation outcome (token-acquisition mechanism)

The requirements' investigation note is confirmed against both the Kiro authentication documentation and this codebase:

- Microsoft Entra ID is **not** a standalone provider button in Kiro. It is reached through Kiro's **"External identity provider"** flow: the operator chooses "Your organization", enters a work email to locate the organization, and completes sign-in with the organization's IdP in the browser. This flow federates through Kiro's own authentication service (`app.kiro.dev` / `prod.us-east-1.auth.desktop.kiro.dev`) and yields a **Kiro refresh token** — the same shape the social providers (Google/GitHub) produce. It does **not** use the AWS OIDC IAM Identity Center endpoints.
- In this codebase, `auth.RefreshToken` (in `auth/oidc.go`) branches on `Account.AuthMethod`:
  - `AuthMethod == "social"` → `refreshSocialToken(refreshToken)` → POSTs `{refreshToken}` to `https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken`. **Requires only a refresh token.**
  - otherwise → `refreshOIDCToken(refreshToken, clientId, clientSecret, region)` → POSTs to `https://oidc.{region}.amazonaws.com/token`. **Requires `clientId`/`clientSecret`.**
- Because the External IdP flow returns only a Kiro refresh token (no OIDC `clientId`/`clientSecret`), an `MS365_Account` **maps to the existing `social` refresh branch**, with a descriptive `Provider` value of `MicrosoftEntra`. **No new refresh branch is required** (Requirement 4.7 is satisfied by reuse; the conditional "WHERE the existing branches do not cover" does not trigger).

### Design decisions and rationale

| Decision | Rationale |
|---|---|
| `MS365_Account.AuthMethod = "social"`, `Provider = "MicrosoftEntra"` | The External IdP token is refresh-token-only, which is exactly what the `social` branch handles. Reusing `social` means token refresh, pooling, failover, and proxying all work with zero changes to those subsystems. |
| Session-based browser flow modeled on `IamSsoSession` (`auth/iam_sso.go`) | Requirement 2 mandates a server-side `Login_Session` with a session id, an authorization URL, and a 600-second expiry. The IAM SSO start/complete pattern already implements exactly this shape (start returns `sessionId`/`authorizeUrl`/`expiresIn=600`; complete exchanges and deletes the session). |
| No new `Account` struct fields | MS365 reuses `AccessToken`, `RefreshToken`, `AuthMethod`, `Provider`, `ExpiresAt`, `ID`, `MachineId`, `Enabled`. The config JSON schema is unchanged, so old config files load unchanged (Requirement 8.3/8.4 satisfied trivially). |
| New `auth/ms365.go` + two handlers (`/auth/ms365/start`, `/auth/ms365/complete`) | Keeps the new flow isolated and analogous to the existing `apiStartIamSso`/`apiCompleteIamSso` handlers, minimizing edits to shared code. |
| Account creation reuses `config.AddAccount` + `h.pool.Reload()` | Every existing method appends through `config.AddAccount` and reloads the pool; MS365 follows the same path so the account immediately becomes poolable. |

## Architecture

The feature spans three layers, each mirroring an existing, proven counterpart. Only the shaded "new" pieces are added; everything downstream (refresh, pool, proxy, failover) is reused unchanged.

```mermaid
flowchart TD
    subgraph Web["Web Admin Panel (web/)"]
        A1["Add Account dialog<br/>modalAdd() + methodCard('ms365')"]
        A2["MS365 login view<br/>modalMs365()"]
        A3["startMs365Login() / completeMs365Login()"]
        A4["Locale_Store<br/>en.json / zh.json / index-legacy.html tables"]
    end

    subgraph Backend["Auth Backend (proxy/ + auth/)"]
        B1["apiStartMs365 (proxy/handler.go)"]
        B2["apiCompleteMs365 (proxy/handler.go)"]
        B3["auth/ms365.go<br/>StartMs365Login / CompleteMs365Login<br/>Ms365Session store (10 min TTL)"]
        B4["config.AddAccount (config/config.go)"]
    end

    subgraph Shared["Reused, unchanged"]
        C1["auth.RefreshToken → refreshSocialToken<br/>(auth/oidc.go)"]
        C2["AccountPool (pool/account.go)"]
        C3["ensureValidToken + handleAccountFailure<br/>(proxy/handler.go, proxy/account_failover.go)"]
        C4["config.UpdateAccountToken"]
    end

    A1 --> A2 --> A3
    A3 -->|POST /admin/api/auth/ms365/start| B1 --> B3
    A3 -->|POST /admin/api/auth/ms365/complete| B2 --> B3
    B3 -->|social refresh token| C1
    B2 --> B4
    B4 --> C2
    C2 --> C3
    C3 --> C1
    C1 --> C4
    A4 -.->|t(key)| A1
    A4 -.->|t(key)| A2
```

### Request flow (happy path)

```mermaid
sequenceDiagram
    participant Op as Operator
    participant UI as Admin Panel (app.js)
    participant BE as Auth Backend
    participant KA as Kiro auth service
    participant Cfg as config store
    participant Pool as AccountPool

    Op->>UI: Select "Enterprise SSO - Microsoft 365"
    UI->>UI: modalMs365() renders login view
    Op->>UI: Click "Start Login"
    UI->>BE: POST /auth/ms365/start
    BE->>BE: StartMs365Login() creates Ms365Session (TTL 600s)
    BE-->>UI: { sessionId, authorizeUrl, expiresIn: 600 }
    UI->>Op: Show authorizeUrl (Open / Copy)
    Op->>KA: Complete org sign-in in browser (Entra ID)
    KA-->>Op: Redirect/callback with auth result
    Op->>UI: Paste callback result, click "Complete Login"
    UI->>BE: POST /auth/ms365/complete { sessionId, callback }
    BE->>BE: CompleteMs365Login() validates session
    BE->>KA: Exchange result → Kiro refresh token
    BE->>KA: refreshSocialToken → accessToken + expiresIn
    BE->>Cfg: AddAccount(AuthMethod=social, Provider=MicrosoftEntra, Enabled=true)
    BE->>Pool: Reload()
    BE-->>UI: { success: true, account }
    UI->>Op: Toast success; refresh account list
```

### Endpoints

Two new admin API routes are registered in the `handleAdminAPI` switch in `proxy/handler.go`, adjacent to the existing `/auth/iam-sso/*` routes:

| Route | Method | Handler | Purpose |
|---|---|---|---|
| `/auth/ms365/start` | POST | `apiStartMs365` | Create a `Login_Session`; return `sessionId`, `authorizeUrl`, `expiresIn`. |
| `/auth/ms365/complete` | POST | `apiCompleteMs365` | Validate the session, exchange for tokens, create the `MS365_Account`. |

(The web panel reaches these under the `/admin/api` prefix, e.g. `POST /admin/api/auth/ms365/start`, consistent with existing calls.)

## Components and Interfaces

### 1. `auth/ms365.go` (new)

Mirrors `auth/iam_sso.go`. Owns the session store and the two flow functions.

```go
// Ms365Session holds the short-lived server-side state for one MS365 login.
type Ms365Session struct {
    State     string    // CSRF/anti-forgery value echoed by the callback
    ExpiresAt time.Time // creation time + 10 minutes
    // additional flow fields (e.g. code verifier) as required by the
    // External IdP exchange, analogous to IamSsoSession
}

// StartMs365Login creates a session and returns its id, the browser
// authorization URL, and the expiry in seconds (600).
func StartMs365Login() (sessionID, authorizeURL string, expiresIn int, err error)

// CompleteMs365Login validates the session, exchanges the browser result
// for a Kiro refresh token, performs one social refresh to obtain a live
// access token + expiry, then deletes the session.
// Returns: accessToken, refreshToken, expiresIn, error.
func CompleteMs365Login(sessionID, callback string) (accessToken, refreshToken string, expiresIn int, err error)
```

Session storage follows the `IamSsoSession` pattern exactly: a package-level `map[string]*Ms365Session` guarded by a `sync.RWMutex`, a `cleanupExpiredMs365Sessions()` sweeper invoked on each start, and deletion of the session on both successful completion and detected expiry. Expiry is `time.Now().Add(10 * time.Minute)` and `StartMs365Login` returns `600` for `expiresIn`.

The token exchange reuses `auth.refreshSocialToken` (already in `auth/oidc.go`) to turn the Kiro refresh token into a live `accessToken` + `expiresIn`, matching how `apiImportCredentials` requires a successful refresh before storing an account.

### 2. `proxy/handler.go` (edited)

Add two handlers analogous to `apiStartIamSso` / `apiCompleteIamSso`.

```go
func (h *Handler) apiStartMs365(w http.ResponseWriter, r *http.Request) {
    sessionID, authorizeURL, expiresIn, err := auth.StartMs365Login()
    // 500 on error; else JSON { sessionId, authorizeUrl, expiresIn }
}

func (h *Handler) apiCompleteMs365(w http.ResponseWriter, r *http.Request) {
    // decode { sessionId, callback }
    accessToken, refreshToken, expiresIn, err := auth.CompleteMs365Login(req.SessionID, req.Callback)
    if err != nil {
        w.WriteHeader(400)                // invalid/expired session, denied auth, exchange failure
        json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
        return
    }
    email, _, _ := auth.GetUserInfo(accessToken)
    account := config.Account{
        ID:           auth.GenerateAccountID(),
        Email:        email,
        AccessToken:  accessToken,
        RefreshToken: refreshToken,
        AuthMethod:   "social",
        Provider:     "MicrosoftEntra",
        ExpiresAt:    time.Now().Unix() + int64(expiresIn),
        Enabled:      true,
        MachineId:    config.GenerateMachineId(),
    }
    if err := config.AddAccount(account); err != nil {
        w.WriteHeader(500); /* error */ return   // stored set unchanged on failure
    }
    h.pool.Reload()
    // JSON { success: true, account: { id, email } }
}
```

The two routes are added to the admin API switch next to `/auth/iam-sso/*`.

### 3. Web panel (`web/app.js`, edited)

- **`METHOD_ICONS`**: add `ms365: 'fa-brands fa-microsoft'` (or a fallback solid icon).
- **`modalAdd(title, body)`**: append one more `methodCard('ms365', t('modal.ms365Title'), t('modal.ms365Desc'))` so the dialog shows seven cards.
- **`showModal(type, ...)`**: add `else if (type === 'ms365') modalMs365(title, body);`.
- **`modalMs365(title, body)`** (new): renders the MS365 login view, structurally like `modalIam` — a description, a "Start Login" button, a step-2 block (hidden until start) showing the authorization URL with Open/Copy buttons and a callback input, and a "Complete Login" button. All strings come from `t('ms365.*')`.
- **`startMs365Login()` / `completeMs365Login()`** (new): mirror `startIamSso`'s two-phase logic — first call `POST /auth/ms365/start`, reveal the URL and store the session id in a module-scoped `ms365Session`; on the second click call `POST /auth/ms365/complete`. On `d.success`, `closeModal(); loadAccounts(); loadStats();` and toast success; on error, `toastError` with the backend's `d.error` while leaving the view open for retry.
- **`closeModal()`**: reset `ms365Session = ''` alongside the existing `iamSession`/`builderIdSession` resets.

Selecting any method calls `showModal(type)`, which overwrites `modalBody.innerHTML`, guaranteeing only one method's view is visible at a time (Requirement 1.4).

### 4. Legacy panel (`web/index-legacy.html`, edited)

- Add one card in the `type === 'add'` template calling `showModal('ms365')`, alongside the existing six cards.
- Add an `else if (type === 'ms365')` branch rendering the login view and wiring inline `onclick` handlers to new `startMs365Login()` / `completeMs365Login()` functions defined in the inline script (mirroring the inline `startIamSso` equivalent).

### 5. Localization (`Locale_Store`, edited)

New keys added to **all four** tables: `web/locales/en.json`, `web/locales/zh.json`, and the `zh` and `en` blocks of the inline `i18n` object in `web/index-legacy.html`. Keys use one identical set across every table:

| Key | Purpose |
|---|---|
| `modal.ms365Title` | Method card title ("Enterprise SSO - Microsoft 365") |
| `modal.ms365Desc` | Method card description |
| `ms365.startLogin` | Start button label |
| `ms365.loginUrl` | Authorization URL field label |
| `ms365.completeLogin` | Instruction to finish sign-in in the browser |
| `ms365.callbackUrl` | Callback/result input label |
| `ms365.complete` | Complete button label |
| `ms365.success` | Success toast |
| `ms365.errorDenied` | "Authorization was denied" message |
| `ms365.errorExchange` | Token-exchange-failed message |
| `ms365.errorSession` | Invalid/expired session message |

The `t(key, ...args)` helper resolves `active[key] || fallback[key] || key`. In `app.js` the fallback dictionary is `zh`; the legacy panel falls back to `en`. Because every key above is defined in **both** locales in **all** tables, the fallback path is never exercised in practice, and a genuinely missing key renders the key identifier rather than empty text (Requirement 1.6). Switching language re-runs `applyTranslations()` + `renderAccounts()` (via `setLang`), re-rendering any open MS365 labels in the new language (Requirement 7.7).

## Data Models

### `config.Account` (unchanged struct; MS365 field usage)

No fields are added. An `MS365_Account` is a standard `Account` with these values:

| Field | MS365 value | Notes |
|---|---|---|
| `ID` | UUID v4 (`auth.GenerateAccountID()`) | Unique among stored accounts |
| `Email` | from `auth.GetUserInfo(accessToken)` | Best-effort; may be empty |
| `AccessToken` | live token from social refresh | Non-empty |
| `RefreshToken` | Kiro refresh token from External IdP flow | Non-empty |
| `AuthMethod` | `"social"` | Selects `refreshSocialToken` |
| `Provider` | `"MicrosoftEntra"` | Display/label only; distinguishes MS365 in the UI |
| `ClientID` / `ClientSecret` / `Region` | empty | Not used by the social branch |
| `ExpiresAt` | `now + expiresIn` (Unix seconds) | Strictly greater than creation time |
| `MachineId` | UUID v4 (`config.GenerateMachineId()`) | Request tracking |
| `Enabled` | `true` | Joins the pool on `Reload()` |

Because `Provider` is an already-existing optional field and `AuthMethod="social"` is an already-recognized value, configuration files written before this feature deserialize with no changes and no new defaults (Requirement 8.3/8.4).

### `Ms365Session` (new, in-memory only)

Short-lived, never persisted to disk. Stored in a package-level map keyed by `sessionID`, with a 10-minute TTL and a cleanup sweep on each new session start — identical lifecycle to `IamSsoSession`.

### AuthMethod → refresh-branch mapping (authoritative)

| Stored `AuthMethod` | Refresh function | Endpoint | Used by |
|---|---|---|---|
| `social` | `refreshSocialToken` | `prod.us-east-1.auth.desktop.kiro.dev/refreshToken` | Social (Google/GitHub), **MS365** |
| `idc` (default) | `refreshOIDCToken` | `oidc.{region}.amazonaws.com/token` | Builder ID, IAM Identity Center, SSO Token |

The `apiImportCredentials` normalization switch (`idc/builderid/enterprise → idc`; `social/google/github → social`) is left intact. The dedicated MS365 completion handler sets `AuthMethod="social"` directly, so it does not depend on that switch; optionally a `microsoft`/`entra` label may be added to the `social` case for the generic credentials-import path without altering existing cases.

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

The MS365 feature is largely UI wiring, localization, and reuse of already-tested subsystems (pool, proxy, failover), which are covered by example and integration tests (see Testing Strategy). The properties below capture the pure, input-varying logic that is genuinely amenable to property-based testing. They were derived from the prework analysis and consolidated to remove redundancy.

### Property 1: Locale resolver never returns empty text

*For any* locale key — whether present in the active dictionary, present only in the fallback dictionary, or absent from both — the `t(key)` resolver returns a non-empty string, and for a key absent from every dictionary it returns exactly the key identifier.

**Validates: Requirements 1.6, 7.6**

### Property 2: Expired or unknown sessions are rejected safely

*For any* completion request that references a session id not present in the store, or a session whose age is at least 600 seconds, `CompleteMs365Login` returns an invalid/expired error, creates no `MS365_Account`, leaves the stored account set unchanged, and leaves no expired session in server-side storage. Conversely, *for any* session whose age is below 600 seconds, the session remains present and eligible for completion.

**Validates: Requirements 2.2, 2.4, 2.5, 6.4**

### Property 3: A completed login yields exactly one correctly-formed account

*For any* starting account set and any successful token exchange, completing an MS365 login appends exactly one account (the stored count grows by one) whose fields satisfy: `AuthMethod == "social"`, `Provider == "MicrosoftEntra"`, non-empty `AccessToken`, non-empty `RefreshToken`, `ExpiresAt` strictly greater than the creation time, `Enabled == true`, an `ID` in UUID v4 format that is distinct from every pre-existing account id, and a `MachineId` in UUID v4 format.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**

### Property 4: Token refresh dispatches strictly on AuthMethod

*For any* account, `auth.RefreshToken` selects exactly the refresh path determined by its `AuthMethod`: an account with `AuthMethod == "social"` (including every `MS365_Account`) is refreshed via the social endpoint and never the OIDC endpoint, and an account with any other `AuthMethod` is refreshed via the OIDC endpoint and never the social endpoint.

**Validates: Requirements 4.1, 4.7, 8.5**

### Property 5: Refresh persistence retains the refresh token when none is returned

*For any* successful refresh result, persisting the update always overwrites the account's access token and expiry with the refreshed values, and overwrites the refresh token if and only if the refresh response returned a non-empty refresh token; when the response returns no new refresh token, the account's existing refresh token is retained unchanged.

**Validates: Requirements 4.2, 4.3, 4.4**

### Property 6: Refresh-failure classification drives the disable/retain lifecycle

*For any* refresh or request error whose message is classified as a credential rejection (401/403, `invalid_grant`, token invalid/expired, unauthorized, and the other auth markers recognized by the failover classifier), `handleAccountFailure` disables the `MS365_Account`; *for any* error classified as transient (not matched by the auth, quota, overage, or suspension classifiers), it leaves the account enabled and its stored credentials unchanged so a later attempt can retry. MS365 accounts follow the identical classification applied to all other account types.

**Validates: Requirements 4.5, 4.6, 5.6**

### Property 7: Loading a pre-existing configuration preserves account fields

*For any* set of previously stored accounts serialized to the configuration file, loading the configuration reproduces every account's `AccessToken`, `RefreshToken`, `ClientID`, `ClientSecret`, `Region`, `AuthMethod`, `Provider`, and `ProfileArn` unchanged and returns no error; account records that omit the optional fields load with those fields at their zero values.

**Validates: Requirements 8.3, 8.4**

## Error Handling

The MS365 flow surfaces failures through the same channels as the existing methods; no new error-reporting infrastructure is introduced.

| Failure | Where detected | Backend behavior | Panel behavior |
|---|---|---|---|
| Invalid JSON on start/complete | `apiStartMs365` / `apiCompleteMs365` | `400` with `{error}` | `toastError` with the message |
| Session not found / expired | `CompleteMs365Login` (session lookup + `ExpiresAt` check) | `400`, no account created, session deleted if expired (Property 2) | Error toast; login view stays open for retry (Req 6.5) |
| Operator denied authorization | `CompleteMs365Login` (callback inspection, like `IamSso`'s `error` param) | `400` with a denial-identifying message (`ms365.errorDenied`) (Req 6.1) | Error toast; view stays open |
| Token exchange / social refresh fails | `CompleteMs365Login` via `refreshSocialToken` | `400` with the failure description (Req 6.2) | Error toast conveying the description (Req 6.3) |
| `config.AddAccount` persist fails | `apiCompleteMs365` | `500` with `{error}`, stored set unchanged (Req 3.6) | Error toast; view stays open |
| Refresh rejected upstream (revoked) | `ensureValidToken` → `handleAccountFailure` → `isAuthErrorMessage` | Account disabled and dropped from pool (Req 4.5) | Account shown disabled with ban reason |
| Refresh transient/network error | `ensureValidToken` → `handleAccountFailure` default | Short cooldown via `RecordError`, account stays enabled, credentials untouched (Req 4.6) | Next request rotates to another account |

On every backend error path the completion handler returns before calling `config.AddAccount`, guaranteeing no account is created and no existing account is modified (Req 6.4). All error strings shown in the panel are resolved through the `Locale_Store`.

## Testing Strategy

A dual approach is used: property-based tests for the input-varying pure logic (the seven properties above) and example/integration tests for UI composition, localization completeness, and reuse of shared subsystems.

### Property-based tests

- **Library**: `pgregory.net/rapid` for the Go backend (already suitable for this Go module; no framework is implemented from scratch). Front-end resolver property (Property 1) is exercised with `fast-check` if a JS test harness is present; otherwise it is validated in Go against the parsed locale JSON.
- **Iterations**: each property test runs a minimum of **100** generated cases.
- **Tagging**: each property test carries a comment of the form
  `// Feature: microsoft-365-sso, Property {number}: {property_text}`.
- **Coverage mapping**:
  - Property 2 & 3 → `auth/ms365_test.go` using an injectable clock/`ExpiresAt` and a mocked social refresh endpoint (reusing the `socialTokenURL` test hook), plus a stubbed account store to observe appends.
  - Property 4 → `auth/oidc_test.go`, recording which of `oidcTokenURL`/`socialTokenURL` is built for generated accounts spanning both `AuthMethod` values.
  - Property 5 → `config` test over `UpdateAccountToken` with generated `(newAccessToken, newRefreshToken, expiresAt)` triples including empty refresh tokens.
  - Property 6 → `proxy` test over `handleAccountFailure` with generated error strings drawn from auth-marker and transient categories.
  - Property 7 → `config` test round-tripping generated account slices through save/load.
  - Property 1 → resolver test over generated present/absent keys.

### Example and integration tests

- **UI composition** (Req 1.1–1.5, 8.1): assert both add dialogs render seven method cards including MS365, and that `showModal('ms365')` renders the login view while replacing any prior view.
- **Localization completeness** (Req 7.1–7.5, 7.7): assert every MS365 key is defined in `en.json`, `zh.json`, and both inline legacy tables, that the key sets match across languages, and that switching language re-renders the labels.
- **Session start shape** (Req 2.1): `StartMs365Login` returns a non-empty `sessionId`, a non-empty `authorizeUrl`, and `expiresIn == 600`.
- **Completion happy path** (Req 2.3): with a mocked Kiro auth/refresh endpoint, completion returns non-empty access and refresh tokens and creates one enabled `social`/`MicrosoftEntra` account.
- **Denial / exchange-failure messages** (Req 6.1–6.3, 6.5): mocked failing completions return descriptive errors that the panel surfaces, with the login view left open.
- **Pool & proxy participation** (Req 5.1–5.5, 8.2, 8.6): integration tests confirming an enabled MS365 account is selectable via `GetNext`, excluded after disable+`Reload`, resolves `ProxyURL` with global fallback, and fails over within `maxAccountRetryAttempts` — reusing the existing pool/failover test suites, which are account-type agnostic.
- **Persist-failure edge case** (Req 3.6): with a storage stub that fails, completion returns an error and the stored account list is unchanged.
