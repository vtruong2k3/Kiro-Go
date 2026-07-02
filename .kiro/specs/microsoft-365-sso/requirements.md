# Requirements Document

## Introduction

This feature adds a new account login method, "Enterprise SSO - Microsoft 365", to the Kiro-Go admin web panel's "Add Account" dialog. Kiro-Go is a Go proxy that converts Kiro accounts into OpenAI/Anthropic-compatible APIs, with multi-account pooling, streaming, automatic token refresh, and a web admin panel.

The "Add Account" dialog currently supports six login methods: AWS Builder ID, IAM Identity Center (Enterprise SSO), SSO Token, Kiro Local Cache, Credentials JSON, and Kiro Web Cookie. This feature introduces a seventh method that lets an operator register a Microsoft 365 / Entra ID (Azure AD) tenant account. The new account must be stored with authentication metadata that enables the existing token-refresh mechanism, participate in the multi-account pool and proxy request flow like every other account type, and expose localized UI strings in both English and Chinese. All existing login methods and previously stored accounts must continue to function without change.

> **Design-phase investigation note (grounded in Kiro official docs):** Per Kiro's [Authentication methods documentation](https://kiro.dev/docs/getting-started/authentication/), Microsoft Entra ID is not a standalone provider button; it is reached through the **"External identity provider"** flow: the user chooses "Your organization", enters their work email to locate the organization, and completes sign-in with the organization's IdP (Microsoft Entra ID or Okta) in the browser. This flow federates through Kiro's own authentication service (`app.kiro.dev` / `auth.desktop.kiro.dev`) and yields a Kiro refresh token, the same way the social providers (Google/GitHub) do — it does **not** go through the AWS OIDC IAM Identity Center endpoints.
>
> The separate [Microsoft Entra IdP setup doc](https://kiro.dev/docs/enterprise/identity-provider/microsoft-entra/) covers **administrator-side configuration only** (Entra app registration, SCIM provisioning, and creating a Kiro profile in the AWS console); it is not the client token-acquisition flow.
>
> Mapping to this codebase: `proxy/handler.go` normalizes `AuthMethod` so that `social`/`google`/`github` refresh via `prod.us-east-1.auth.desktop.kiro.dev/refreshToken` (refresh token only), while `idc`/`builderid`/`enterprise` refresh via `oidc.{region}.amazonaws.com/token` (require clientId/clientSecret). `refreshSocialToken` in `auth/oidc.go` needs only a refresh token, which matches what the External IdP flow returns. Therefore an MS365_Account SHOULD map to the **`social` refresh branch** (with a `Provider` value such as `Enterprise` or a new `MicrosoftEntra` label), reusing the existing refresh mechanism, rather than the `idc` OIDC path. Requirement 3 and Requirement 4 depend on the design confirming this mapping and the exact means of obtaining the initial refresh token (browser organization sign-in).

## Glossary

- **Kiro_Go**: The Go proxy application, including its backend HTTP handlers and web admin panel, that this feature extends.
- **Admin_Panel**: The web administration interface served by Kiro_Go (`web/app.js` and the legacy `web/index-legacy.html`).
- **Add_Account_Dialog**: The modal in the Admin_Panel that presents the available account login methods.
- **MS365_Method**: The new "Enterprise SSO - Microsoft 365" login method being added, covering Microsoft 365 / Entra ID / Azure AD tenant accounts.
- **MS365_Account**: An Account created through the MS365_Method.
- **Account**: The account record defined by the `Account` struct in `config/config.go`, including fields such as `AccessToken`, `RefreshToken`, `ClientID`, `ClientSecret`, `Region`, `AuthMethod`, `Provider`, `ProfileArn`, and `ProxyURL`.
- **Auth_Backend**: The backend HTTP handlers for authentication and account management in the `proxy/` package and the flow implementations in the `auth/` package.
- **Token_Refresh**: The `RefreshToken(account)` function in `auth/oidc.go` that renews an Account's access token, branching on `AuthMethod`.
- **Account_Pool**: The multi-account pooling and selection component in the `pool/` package that dispatches proxy requests across enabled accounts.
- **Locale_Store**: The localization string files `web/locales/en.json` (English) and `web/locales/zh.json` (Chinese), plus the inline locale tables in `web/index-legacy.html`.
- **Login_Session**: A short-lived server-side authentication session created when an MS365_Method login is initiated, analogous to `IamSsoSession` in `auth/iam_sso.go`.

## Requirements

### Requirement 1: MS365 method appears in the Add Account dialog

**User Story:** As a Kiro-Go operator, I want an "Enterprise SSO - Microsoft 365" option in the Add Account dialog, so that I can register a Microsoft 365 / Entra ID tenant account.

#### Acceptance Criteria

1. WHEN the Add_Account_Dialog is opened in the `web/app.js` panel, THE Admin_Panel SHALL display the MS365_Method as one selectable option together with the six existing methods, for a total of seven selectable login-method options.
2. WHEN the Add_Account_Dialog is opened in the `web/index-legacy.html` panel, THE Admin_Panel SHALL display the MS365_Method as one selectable option together with the six existing methods, for a total of seven selectable login-method options.
3. WHEN the operator selects the MS365_Method option, THE Admin_Panel SHALL display a dedicated MS365_Method login view that presents the controls the operator uses to initiate the MS365_Method login.
4. WHEN the operator selects the MS365_Method option after another login method's view is shown, THE Admin_Panel SHALL replace the previously shown login view with the MS365_Method login view so that only one method's login view is visible at a time.
5. WHEN the Admin_Panel renders the MS365_Method option, THE Admin_Panel SHALL display a title and a description resolved from the Locale_Store for the active language.
6. IF the Locale_Store does not contain a title or description string for the MS365_Method option in the active language, THEN THE Admin_Panel SHALL render the MS365_Method option using its locale key identifier in place of the missing string rather than rendering empty text.

### Requirement 2: Initiate and complete Microsoft 365 authentication

**User Story:** As a Kiro-Go operator, I want to start and finish a Microsoft 365 / Entra ID tenant login from the panel, so that Kiro-Go obtains valid credentials for the account.

#### Acceptance Criteria

1. WHEN the operator initiates an MS365_Method login, THE Auth_Backend SHALL create a Login_Session and return a unique session identifier, a browser authorization URL, and the session expiry duration in seconds.
2. WHILE a Login_Session is active, THE Auth_Backend SHALL retain the session state required to complete the login for 600 seconds (10 minutes) from session creation.
3. WHEN the operator submits the completed authentication result for an active Login_Session to the Auth_Backend, THE Auth_Backend SHALL exchange it for a Kiro-compatible access token and refresh token.
4. IF the operator submits a completion request referencing a Login_Session that does not exist or has expired, THEN THE Auth_Backend SHALL create no MS365_Account and return an error response identifying the session as invalid or expired.
5. WHEN a Login_Session is older than 600 seconds (10 minutes), THE Auth_Backend SHALL remove the expired Login_Session from server-side storage.

### Requirement 3: Store the MS365 account with correct authentication metadata

**User Story:** As a Kiro-Go operator, I want a completed Microsoft 365 login to create a stored account with the correct metadata, so that the account is usable and its tokens can be refreshed.

#### Acceptance Criteria

1. WHEN an MS365_Method login completes successfully, THE Auth_Backend SHALL create exactly one MS365_Account and append it to the stored account set through the existing account storage in `config/config.go`.
2. WHEN an MS365_Account is created, THE Auth_Backend SHALL set the account `AuthMethod` field and any associated provider metadata to values that the Token_Refresh function uses to select the correct refresh path for Microsoft 365 accounts.
3. WHEN an MS365_Account is created, THE Auth_Backend SHALL populate a non-empty `AccessToken`, a non-empty `RefreshToken`, and an `ExpiresAt` Unix-seconds timestamp later than the account creation time.
4. WHEN an MS365_Account is created, THE Auth_Backend SHALL assign the account a UUID v4 identifier unique among stored accounts and a UUID v4 machine identifier.
5. WHEN an MS365_Account is created, THE Auth_Backend SHALL mark the account as enabled so that it joins the Account_Pool.
6. IF persisting a newly created MS365_Account to storage fails, THEN THE Auth_Backend SHALL return an error and leave the stored account set unchanged.

> The specific set of metadata fields and their values (for example, whether the MS365_Method reuses the `idc` OIDC path with `ClientID`/`ClientSecret`/`Region`, reuses the `social` path, or introduces a new `AuthMethod` value and refresh branch) depends on the token-acquisition mechanism resolved in design. Design MUST define these values before implementation.

### Requirement 4: Token refresh for Microsoft 365 accounts

**User Story:** As a Kiro-Go operator, I want Microsoft 365 account tokens to refresh automatically, so that the account keeps serving requests without manual re-authentication.

#### Acceptance Criteria

1. WHEN the Token_Refresh function is invoked for an MS365_Account, THE Kiro_Go SHALL renew the account's access token using the refresh path selected by the account's `AuthMethod` value and no other refresh branch.
2. WHEN a token refresh for an MS365_Account succeeds, THE Kiro_Go SHALL persist the renewed access token and the updated expiry timestamp to the account record.
3. WHEN a token refresh for an MS365_Account succeeds and the refresh response returns a new refresh token, THE Kiro_Go SHALL persist the new refresh token to the account record.
4. IF a token refresh for an MS365_Account succeeds but the refresh response returns no new refresh token, THEN THE Kiro_Go SHALL retain the account's existing refresh token unchanged.
5. IF a token refresh for an MS365_Account fails because the credentials are rejected or revoked by the identity provider, THEN THE Kiro_Go SHALL disable the account and surface the failure through the same account-disable and error-reporting behavior applied to the existing login methods.
6. IF a token refresh for an MS365_Account fails due to a transient network error or a request timeout rather than credential rejection, THEN THE Kiro_Go SHALL leave the account enabled and its stored credentials unchanged so that a subsequent refresh attempt can retry.
7. WHERE the MS365_Method requires a refresh path that the existing Token_Refresh branches do not cover, THE Kiro_Go SHALL add a refresh branch for the Microsoft 365 `AuthMethod` value without altering the existing branches.

### Requirement 5: MS365 accounts participate in the pool and proxy flow

**User Story:** As a Kiro-Go operator, I want Microsoft 365 accounts to behave like every other account type during proxying, so that they share the same pooling, streaming, and failover behavior.

#### Acceptance Criteria

1. WHILE an MS365_Account is enabled and is neither within a cooldown window nor quota-blocked, THE Account_Pool SHALL make the account eligible for the weighted round-robin selection used in `pool/account.go` on the same terms as other account types.
2. WHEN the Account_Pool selects an MS365_Account for a request, THE Kiro_Go SHALL process the proxy request through the same request-handling path applied to the existing account types, including streaming responses and just-in-time token refresh, and SHALL produce responses whose structure and format are indistinguishable from those produced for the existing account types.
3. WHEN an MS365_Account is disabled, THE Account_Pool SHALL exclude the account from account selection on the next pool reload.
4. WHEN proxy requests are dispatched, THE Kiro_Go SHALL apply the same per-account outbound proxy resolution (`ProxyURL` with global fallback) to MS365_Accounts that it applies to other account types.
5. IF a proxy request using an MS365_Account fails with a retryable error, THEN THE Kiro_Go SHALL fail over to a different account, bounded by the same maximum retry-attempt limit (`maxAccountRetryAttempts` in `proxy/account_failover.go`) applied to other account types.
6. WHEN a proxy request using an MS365_Account fails, THE Kiro_Go SHALL apply the same authentication, quota, overage, and suspension error-classification cooldown and disable lifecycle used by `handleAccountFailure` for the existing account types.

### Requirement 6: Error handling for failed, expired, or denied authentication

**User Story:** As a Kiro-Go operator, I want clear feedback when a Microsoft 365 login does not succeed, so that I can understand and correct the problem.

#### Acceptance Criteria

1. IF the authentication result submitted for an MS365_Method login indicates the operator denied authorization, THEN THE Auth_Backend SHALL return an error response identifying denied authorization as the cause.
2. IF the token exchange for an MS365_Method login fails, THEN THE Auth_Backend SHALL return an error response containing a description of the failure cause.
3. WHEN the Auth_Backend returns an MS365_Method authentication error, THE Admin_Panel SHALL display an error message that conveys the failure description returned by the Auth_Backend.
4. IF an MS365_Method authentication error occurs, THEN THE Kiro_Go SHALL create no new MS365_Account and modify no existing account.
5. WHEN an MS365_Method authentication error has been displayed, THE Admin_Panel SHALL keep the MS365_Method login view available so that the operator can retry the login.

### Requirement 7: Localization for Microsoft 365 UI strings

**User Story:** As a Kiro-Go operator using English or Chinese, I want all Microsoft 365 UI strings localized, so that the new method reads correctly in my chosen language.

#### Acceptance Criteria

1. THE Locale_Store SHALL define an English string for every UI label introduced by the MS365_Method, comprising the method title, the method description, the login view instructions, the action buttons, and the MS365 authentication error messages.
2. THE Locale_Store SHALL define a Chinese string for every UI label introduced by the MS365_Method identified in criterion 1.
3. THE `web/index-legacy.html` inline locale tables SHALL define English and Chinese strings for every UI label introduced by the MS365_Method identified in criterion 1.
4. THE Locale_Store SHALL use identical locale key identifiers for the MS365_Method UI labels across its English and Chinese entries.
5. WHILE the active language is English or Chinese, WHEN the Admin_Panel renders an MS365_Method UI label, THE Admin_Panel SHALL resolve the text through the Locale_Store using the active language.
6. IF the Locale_Store lacks a string for an MS365_Method UI label in the active language, THEN THE Admin_Panel SHALL fall back to the English string for that label and SHALL NOT render an empty label.
7. WHEN the operator switches the active language between English and Chinese, THE Admin_Panel SHALL re-render the MS365_Method UI labels in the newly selected language.

### Requirement 8: Backward compatibility

**User Story:** As a Kiro-Go operator with existing accounts, I want the six existing login methods and my stored accounts to keep working, so that adding the Microsoft 365 method does not disrupt current operation.

#### Acceptance Criteria

1. WHEN the Add_Account_Dialog is opened after the MS365_Method is added, THE Admin_Panel SHALL display the six existing login methods (AWS Builder ID, IAM Identity Center, SSO Token, Kiro Local Cache, Credentials JSON, and Kiro Web Cookie) as selectable options in both the `web/app.js` panel and the `web/index-legacy.html` panel.
2. WHEN the operator selects one of the six existing login methods, THE Admin_Panel SHALL present that method's login view unchanged and complete account creation through the same flow used before the MS365_Method was added.
3. WHEN Kiro_Go loads a configuration file that predates the MS365_Method, THE Kiro_Go SHALL load all existing accounts without error and without modifying their `AccessToken`, `RefreshToken`, `ClientID`, `ClientSecret`, `Region`, `AuthMethod`, `Provider`, and `ProfileArn` fields.
4. IF a loaded configuration file omits configuration fields introduced for the MS365_Method, THEN THE Kiro_Go SHALL load the affected accounts with those fields set to their default empty values and return no error.
5. WHEN the `RefreshToken` function is invoked for an account created by one of the six existing methods, THE Kiro_Go SHALL renew the account using the refresh branch selected by that account's `AuthMethod` value, without altering that branch.
6. WHEN the Account_Pool selects an account created by one of the six existing methods, THE Kiro_Go SHALL process the proxy request through the same request-handling path and the same per-account outbound proxy resolution (`ProxyURL` with global fallback) used before the MS365_Method was added.
