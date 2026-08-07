package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	mgmtClientID     string
	mgmtClientSecret string

	mgmtTokenMu      sync.Mutex
	mgmtTokenCache   string
	mgmtTokenExpires time.Time
)

// pingoneEnvID and pingoneAPIBase are derived from idpIssuer at startup.
// e.g. https://auth.pingone.ca/abc-123/as → envID=abc-123, apiBase=https://api.pingone.ca/v1
var (
	pingoneEnvID   string
	pingoneAPIBase string
)

// initPingOneCoords derives pingoneEnvID, pingoneAPIBase, and idpJwksURL from
// idpIssuer. Must be called after idpIssuer is set.
//
// idpIssuer form: https://auth.pingone.<region>/<env-id>/as
func initPingOneCoords() error {
	withoutScheme := strings.TrimPrefix(idpIssuer, "https://")
	parts := strings.SplitN(withoutScheme, "/", 3)
	if len(parts) < 2 || parts[1] == "" {
		return fmt.Errorf("cannot parse env ID from IDP_ISSUER %q — expected https://auth.pingone.<region>/<env-id>/as", idpIssuer)
	}
	pingoneEnvID = parts[1]

	apiHost := strings.Replace(parts[0], "auth.", "api.", 1)
	pingoneAPIBase = "https://" + apiHost + "/v1"

	idpJwksURL = idpIssuer + "/jwks"
	return nil
}

// resolveUserEmail fetches a management token using client_credentials, then
// looks up the user by sub in the PingOne management API and returns their email.
func resolveUserEmail(sub string) (string, error) {
	tok, err := getMgmtToken()
	if err != nil {
		return "", fmt.Errorf("management token: %w", err)
	}

	filter := url.QueryEscape(fmt.Sprintf(`sub eq "%s"`, sub))
	apiURL := fmt.Sprintf("%s/environments/%s/users?filter=%s", pingoneAPIBase, pingoneEnvID, filter)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build user lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("user lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("user lookup returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Embedded struct {
			Users []struct {
				Email string `json:"email"`
			} `json:"users"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode user lookup response: %w", err)
	}
	if len(result.Embedded.Users) == 0 {
		return "", fmt.Errorf("no user found with sub %q", sub)
	}

	email := result.Embedded.Users[0].Email
	if email == "" {
		return "", fmt.Errorf("user %q has no email", sub)
	}
	log.Printf("resolved sub=%s → email=%s", sub, email)
	return email, nil
}

// getMgmtToken returns a cached management token, refreshing if expired.
func getMgmtToken() (string, error) {
	mgmtTokenMu.Lock()
	defer mgmtTokenMu.Unlock()

	if mgmtTokenCache != "" && time.Now().Before(mgmtTokenExpires) {
		return mgmtTokenCache, nil
	}

	tokenURL := idpIssuer + "/token"
	body := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {"p1:read:user"},
	}
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(mgmtClientID, mgmtClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, respBody)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	mgmtTokenCache = tok.AccessToken
	mgmtTokenExpires = time.Now().Add(time.Duration(tok.ExpiresIn-30) * time.Second)
	return mgmtTokenCache, nil
}
