# Implementation Plan: Microsoft 365 SSO (Enterprise SSO - Microsoft 365)

## Overview

This plan adds the seventh "Enterprise SSO - Microsoft 365" (`MS365_Method`) login method by reusing proven, existing subsystems. The backend is Go (`auth/`, `proxy/`, `config/`); the admin panel is JavaScript/HTML (`web/`). Per the design, MS365 accounts map to the existing `social` refresh branch (`AuthMethod="social"`, `Provider="MicrosoftEntra"`), so token refresh, pooling, failover, and proxying are reused without modification.

Work proceeds bottom-up: the isolated `auth/ms365.go` session/flow first, then the two HTTP handlers and account creation, then validation of the reused refresh/persistence/pool/failover paths, then the `app.js` and legacy-panel UI wiring, and finally localization. Each step builds on the previous and ends wired into the running admin flow. Property tests validate the pure, input-varying logic (the seven correctness properties); example and integration tests cover UI composition, localization completeness, and reuse of shared subsystems.

## Tasks

- [x] 1. Implement the MS365 auth flow (`auth/ms365.go`)
  - [x] 1.1 Implement the `Ms365Session` store and `StartMs365Login`
    - Create `auth/ms365.go` mirroring `auth/iam_sso.go`: define the `Ms365Session` struct (state/anti-forgery value, `ExpiresAt`, and any code-verifier fields needed by the External IdP exchange)
    - Add a package-level `map[string]*Ms365Session` guarded by a `sync.RWMutex` and a `cleanupExpiredMs365Sessions()` sweeper
    - Implement `StartMs365Login() (sessionID, authorizeURL string, expiresIn int, err error)`: generate a UUID session id, set `ExpiresAt = time.Now().Add(10 * time.Minute)`, build the browser authorization URL, invoke the cleanup sweep, and return `expiresIn = 600`
    - _Requirements: 2.1, 2.2_
  - [x] 1.2 Implement `CompleteMs365Login`
    - Implement `CompleteMs365Login(sessionID, callback string) (accessToken, refreshToken string, expiresIn int, err error)`: look up the session, reject when missing or when age >= 600s, inspect the callback for denied authorization, exchange the browser result for a Kiro refresh token, and call `auth.refreshSocialToken` to obtain a live access token + expiry
    - Delete the session on both successful completion and detected expiry; return a distinct invalid/expired error, a denial error, and an exchange-failure error carrying the failure description
    - _Requirements: 2.3, 2.4, 2.5, 6.1, 6.2_
  - [x] 1.3 Write property test for session lifecycle
    - **Property 2: Expired or unknown sessions are rejected safely** — generate unknown session ids and sessions with age >= 600s and < 600s; assert rejection with an invalid/expired error, no expired session left in the store, and that fresh sessions remain eligible (`auth/ms365_test.go`, `pgregory.net/rapid`, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 2`)
    - **Validates: Requirements 2.2, 2.4, 2.5, 6.4**
  - [x] 1.4 Write example tests for start shape and completion happy path
    - Assert `StartMs365Login` returns a non-empty `sessionId`, a non-empty `authorizeUrl`, and `expiresIn == 600`
    - With the social refresh endpoint mocked via the `socialTokenURL` test hook, assert `CompleteMs365Login` returns non-empty access and refresh tokens (`auth/ms365_test.go`)
    - _Requirements: 2.1, 2.3_

- [x] 2. Implement the HTTP handlers, routes, and account creation (`proxy/handler.go`)
  - [x] 2.1 Implement `apiStartMs365` / `apiCompleteMs365` and register the routes
    - Add `apiStartMs365` (calls `auth.StartMs365Login`, returns JSON `{sessionId, authorizeUrl, expiresIn}`, `500` on error) and `apiCompleteMs365` (decodes `{sessionId, callback}`, calls `auth.CompleteMs365Login`, `400` on invalid/expired/denied/exchange failure)
    - On success build the `MS365_Account` (`ID = auth.GenerateAccountID()`, `Email` from `auth.GetUserInfo`, non-empty `AccessToken`/`RefreshToken`, `AuthMethod = "social"`, `Provider = "MicrosoftEntra"`, `ExpiresAt = time.Now().Unix() + expiresIn`, `Enabled = true`, `MachineId = config.GenerateMachineId()`), persist via `config.AddAccount`, return `500` and leave the stored set unchanged on persist failure, then call `h.pool.Reload()` and return `{success, account}`
    - Register `/auth/ms365/start` and `/auth/ms365/complete` (POST) in the `handleAdminAPI` switch next to the `/auth/iam-sso/*` routes
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 6.2, 6.3, 6.4_
  - [x] 2.2 Write property test for account creation correctness
    - **Property 3: A completed login yields exactly one correctly-formed account** — for any starting account set and any successful exchange, assert the stored count grows by exactly one and the new account has `AuthMethod == "social"`, `Provider == "MicrosoftEntra"`, non-empty `AccessToken`/`RefreshToken`, `ExpiresAt` strictly greater than creation time, `Enabled == true`, a UUID v4 `ID` distinct from all pre-existing ids, and a UUID v4 `MachineId` (>= 100 cases, tagged `// Feature: microsoft-365-sso, Property 3`)
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5**
  - [x] 2.3 Write example tests for error paths
    - With a storage stub that fails, assert completion returns an error and the stored account list is unchanged (Req 3.6)
    - With mocked failing completions, assert denial and exchange-failure responses return descriptive errors and create no account (Req 6.1, 6.2, 6.4); confirm the response conveys the description surfaced to the panel (Req 6.3)
    - _Requirements: 3.6, 6.1, 6.2, 6.3, 6.5_

- [x] 3. Checkpoint - backend flow
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Validate reuse of refresh, persistence, pooling, and failover for MS365 accounts
  - [x] 4.1 Add the `microsoft`/`entra` label to the `social` normalization case
    - In the `apiImportCredentials` `AuthMethod` normalization switch (`proxy/handler.go`), add `microsoft`/`entra` to the `social` case so the generic credentials-import path recognizes MS365, leaving the `idc`/`builderid`/`enterprise` and existing `social`/`google`/`github` cases unchanged
    - _Requirements: 3.2, 4.7_
  - [x] 4.2 Write property test for refresh dispatch
    - **Property 4: Token refresh dispatches strictly on AuthMethod** — generate accounts spanning both `AuthMethod` values and assert `auth.RefreshToken` builds the social endpoint (`socialTokenURL`) for `social` accounts (including MS365) and never the OIDC endpoint, and the OIDC endpoint (`oidcTokenURL`) otherwise and never social (`auth/oidc_test.go`, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 4`)
    - **Validates: Requirements 4.1, 4.7, 8.5**
  - [x] 4.3 Write property test for refresh-token persistence
    - **Property 5: Refresh persistence retains the refresh token when none is returned** — over `config.UpdateAccountToken` with generated `(newAccessToken, newRefreshToken, expiresAt)` triples (including empty refresh tokens), assert access token and expiry are always overwritten and the refresh token is overwritten iff a non-empty new refresh token is returned, otherwise retained (`config` test, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 5`)
    - **Validates: Requirements 4.2, 4.3, 4.4**
  - [x] 4.4 Write property test for refresh-failure classification
    - **Property 6: Refresh-failure classification drives the disable/retain lifecycle** — over `handleAccountFailure` with generated error strings from auth-marker and transient categories, assert credential-rejection errors disable the MS365 account while transient errors leave it enabled with credentials unchanged, identical to other account types (`proxy` test, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 6`)
    - **Validates: Requirements 4.5, 4.6, 5.6**
  - [x] 4.5 Write property test for configuration load fidelity
    - **Property 7: Loading a pre-existing configuration preserves account fields** — round-trip generated account slices through save/load and assert `AccessToken`, `RefreshToken`, `ClientID`, `ClientSecret`, `Region`, `AuthMethod`, `Provider`, and `ProfileArn` are reproduced unchanged with no error, and accounts omitting optional fields load with zero values (`config` test, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 7`)
    - **Validates: Requirements 8.3, 8.4**
  - [x] 4.6 Write integration tests for pool and proxy participation
    - Assert an enabled MS365 account is selectable via `GetNext`, is excluded after disable + `Reload`, resolves `ProxyURL` with global fallback, and fails over within `maxAccountRetryAttempts` — reusing the account-type-agnostic pool/failover suites
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 8.2, 8.6_

- [x] 5. Checkpoint - refresh, persistence, and pool reuse
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Wire the MS365 login view into the modern panel (`web/app.js`)
  - [x] 6.1 Add the method card, view routing, and view renderer
    - Add `ms365: 'fa-brands fa-microsoft'` (with a solid fallback) to `METHOD_ICONS`; append a seventh `methodCard('ms365', t('modal.ms365Title'), t('modal.ms365Desc'))` in `modalAdd`; add `else if (type === 'ms365') modalMs365(...)` in `showModal`; reset `ms365Session = ''` in `closeModal`
    - Implement `modalMs365(title, body)` structurally like `modalIam`: description, "Start Login" button, a hidden step-2 block with the authorization URL (Open/Copy) and a callback input, and a "Complete Login" button, all strings via `t('ms365.*')`
    - _Requirements: 1.1, 1.3, 1.4, 1.5, 8.1_
  - [x] 6.2 Implement `startMs365Login` and `completeMs365Login`
    - `startMs365Login`: `POST /auth/ms365/start`, reveal the URL, store the session id in module-scoped `ms365Session`
    - `completeMs365Login`: `POST /auth/ms365/complete`; on `d.success` run `closeModal(); loadAccounts(); loadStats();` and toast success; on error `toastError(d.error)` and leave the view open for retry
    - _Requirements: 2.1, 2.3, 6.3, 6.5_
  - [x] 6.3 Write example tests for dialog composition
    - Assert the add dialog renders seven method cards including MS365, and that `showModal('ms365')` renders the MS365 login view while replacing any previously shown method view
    - _Requirements: 1.1, 1.4_

- [x] 7. Wire the MS365 login view into the legacy panel (`web/index-legacy.html`)
  - [x] 7.1 Add the legacy card, view branch, and inline handlers
    - Add one MS365 card calling `showModal('ms365')` in the `type === 'add'` template alongside the existing six
    - Add an `else if (type === 'ms365')` branch rendering the login view and wire inline `onclick` handlers to inline `startMs365Login()` / `completeMs365Login()` functions mirroring the inline `startIamSso` equivalent
    - _Requirements: 1.2, 1.3, 1.4, 1.5, 2.1, 2.3, 6.3, 6.5, 8.1_

- [x] 8. Add localization for all MS365 UI strings
  - [x] 8.1 Add MS365 keys to `web/locales/en.json` and `web/locales/zh.json`
    - Add the identical key set to both files: `modal.ms365Title`, `modal.ms365Desc`, `ms365.startLogin`, `ms365.loginUrl`, `ms365.completeLogin`, `ms365.callbackUrl`, `ms365.complete`, `ms365.success`, `ms365.errorDenied`, `ms365.errorExchange`, `ms365.errorSession`
    - _Requirements: 7.1, 7.2, 7.4, 7.5, 7.7_
  - [x] 8.2 Add MS365 keys to the legacy inline locale tables
    - Add the same key set with English and Chinese values to both the `en` and `zh` blocks of the inline `i18n` object in `web/index-legacy.html`
    - _Requirements: 7.3, 7.4, 7.5, 7.7_
  - [x] 8.3 Write property test for the locale resolver
    - **Property 1: Locale resolver never returns empty text** — for generated keys present in the active dictionary, present only in the fallback, or absent from both, assert `t(key)` returns a non-empty string and returns exactly the key identifier when absent from every dictionary (`fast-check` if a JS harness exists, otherwise a Go test over the parsed locale JSON, >= 100 cases, tagged `// Feature: microsoft-365-sso, Property 1`)
    - **Validates: Requirements 1.6, 7.6**
  - [x] 8.4 Write example test for localization completeness
    - Assert every MS365 key is defined in `en.json`, `zh.json`, and both inline legacy tables; that the key sets match across languages; and that switching language re-renders the MS365 labels in the newly selected language
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.7_

- [x] 9. Final checkpoint - full feature
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional test tasks and can be skipped for a faster MVP; core implementation tasks are never optional.
- Each task references specific granular requirements for traceability.
- Property tests validate the seven universal correctness properties from the design; each is its own sub-task annotated with its property number and the requirements clauses it checks, placed close to the code it exercises.
- Example and integration tests cover UI composition, localization completeness, and reuse of the pool/proxy/failover subsystems, which are account-type agnostic.
- MS365 introduces no new `Account` struct fields and no new refresh branch; it reuses `AuthMethod="social"` (`Provider="MicrosoftEntra"`), so old configuration files load unchanged.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "6.1", "8.1"] },
    { "id": 1, "tasks": ["1.2", "2.1", "6.2", "7.1"] },
    { "id": 2, "tasks": ["1.3", "1.4", "4.1", "8.2", "6.3"] },
    { "id": 3, "tasks": ["2.2", "2.3", "4.2", "4.3", "4.4", "8.3", "8.4"] },
    { "id": 4, "tasks": ["4.5", "4.6"] }
  ]
}
```
