package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// oidcTokenURL 构造 idc/builderId 刷新 endpoint。测试可替换以拦截网络调用。
var oidcTokenURL = func(region string) string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
}

// socialTokenURL 构造 social 刷新 endpoint。测试可替换以拦截网络调用。
var socialTokenURL = func() string {
	return "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
}

// RefreshToken 刷新 access token
// Returns: accessToken, refreshToken, expiresAt, profileArn, error
func RefreshToken(account *config.Account) (string, string, int64, string, error) {
	// Resolve per-account proxy: account.ProxyURL > global config
	proxyURL := account.ProxyURL
	if proxyURL == "" {
		proxyURL = config.GetProxyURL()
	}
	client := GetAuthClientForProxy(proxyURL)

	if account.AuthMethod == "social" {
		return refreshSocialToken(account.RefreshToken, client)
	}
	if account.AuthMethod == "external_idp" {
		return refreshExternalIdpToken(account.RefreshToken, account.ClientID, account.TokenEndpoint, account.Scopes, client)
	}
	return refreshOIDCToken(account.RefreshToken, account.ClientID, account.ClientSecret, account.Region, client)
}

// refreshExternalIdpToken renews an "external_idp" (Microsoft Entra / org SSO)
// access token by calling the organization's OAuth2 token endpoint directly
// with the standard refresh_token grant (form-encoded, snake_case response).
// It requires the IdP token endpoint, the app clientId, and the scopes that
// were granted at sign-in (including offline_access to receive a rotated
// refresh token).
func refreshExternalIdpToken(refreshToken, clientID, tokenEndpoint, scopes string, client *http.Client) (string, string, int64, string, error) {
	if tokenEndpoint == "" || clientID == "" {
		return "", "", 0, "", fmt.Errorf("external_idp refresh requires tokenEndpoint and clientId")
	}

	form := neturl.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	if scopes != "" {
		form.Set("scope", scopes)
	}

	req, _ := http.NewRequest("POST", tokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	// Microsoft Entra returns snake_case OAuth2 fields.
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	// external_idp accounts have no CodeWhisperer profileArn.
	return result.AccessToken, result.RefreshToken, expiresAt, "", nil
}

// refreshOIDCToken IdC/Builder ID token 刷新
func refreshOIDCToken(refreshToken, clientID, clientSecret, region string, client *http.Client) (string, string, int64, string, error) {
	if clientID == "" || clientSecret == "" {
		return "", "", 0, "", fmt.Errorf("OIDC refresh requires clientId and clientSecret")
	}
	if region == "" {
		region = "us-east-1"
	}

	url := oidcTokenURL(region)

	payload := map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": refreshToken,
		"grantType":    "refresh_token",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	return result.AccessToken, result.RefreshToken, expiresAt, result.ProfileArn, nil
}

// refreshSocialToken Social (GitHub/Google) token 刷新
func refreshSocialToken(refreshToken string, client *http.Client) (string, string, int64, string, error) {
	url := socialTokenURL()

	payload := map[string]string{
		"refreshToken": refreshToken,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	return result.AccessToken, result.RefreshToken, expiresAt, result.ProfileArn, nil
}
