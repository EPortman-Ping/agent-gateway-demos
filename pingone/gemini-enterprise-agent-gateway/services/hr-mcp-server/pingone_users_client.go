package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// pingoneUsersClient calls the PingOne management API to list and create users.
// It uses its own client_credentials token (cached) to authenticate.
type pingoneUsersClient struct {
	envID        string
	apiBase      string
	tokenURL     string
	clientID     string
	clientSecret string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// PingOneUser is a subset of the PingOne user resource used by the tools.
type PingOneUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

func (c *pingoneUsersClient) accessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequest(http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("empty access_token")
	}
	ttl := time.Duration(out.ExpiresIn)*time.Second - 30*time.Second
	if ttl < 10*time.Second {
		ttl = 10 * time.Second
	}
	c.token = out.AccessToken
	c.expires = time.Now().Add(ttl)
	return c.token, nil
}

// ListUsers returns all users in the environment, capped at 100 to keep
// agent context manageable. Increase the limit parameter for larger directories.
func (c *pingoneUsersClient) ListUsers() ([]PingOneUser, error) {
	tok, err := c.accessToken()
	if err != nil {
		return nil, fmt.Errorf("management token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/environments/%s/users?limit=100", c.apiBase, c.envID)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list users request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list users HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Embedded struct {
			Users []PingOneUser `json:"users"`
		} `json:"_embedded"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode list users response: %w", err)
	}
	return result.Embedded.Users, nil
}

// GetUser returns a single PingOne user by their ID or username (email).
// PingOne's filter syntax is used when the value looks like an email.
func (c *pingoneUsersClient) GetUser(idOrEmail string) (*PingOneUser, error) {
	tok, err := c.accessToken()
	if err != nil {
		return nil, fmt.Errorf("management token: %w", err)
	}

	var apiURL string
	if strings.Contains(idOrEmail, "@") {
		// Filter by username (PingOne uses username for email-based lookups).
		apiURL = fmt.Sprintf("%s/environments/%s/users?filter=username+eq+%%22%s%%22",
			c.apiBase, c.envID, url.QueryEscape(idOrEmail))
	} else {
		// Direct ID lookup.
		apiURL = fmt.Sprintf("%s/environments/%s/users/%s", c.apiBase, c.envID, idOrEmail)
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get user request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user HTTP %d: %s", resp.StatusCode, body)
	}

	// If we filtered by username, the response is an _embedded list.
	if strings.Contains(idOrEmail, "@") {
		var result struct {
			Embedded struct {
				Users []PingOneUser `json:"users"`
			} `json:"_embedded"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode filter response: %w", err)
		}
		if len(result.Embedded.Users) == 0 {
			return nil, fmt.Errorf("user %q not found", idOrEmail)
		}
		u := result.Embedded.Users[0]
		return &u, nil
	}

	var u PingOneUser
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("decode user response: %w", err)
	}
	return &u, nil
}

// CreateUser creates a new PingOne user. Returns the created user on success.
func (c *pingoneUsersClient) CreateUser(givenName, familyName, email, username string) (*PingOneUser, error) {
	tok, err := c.accessToken()
	if err != nil {
		return nil, fmt.Errorf("management token: %w", err)
	}

	payload := map[string]any{
		"username":   username,
		"email":      email,
		"givenName":  givenName,
		"familyName": familyName,
	}
	bodyBytes, _ := json.Marshal(payload)

	apiURL := fmt.Sprintf("%s/environments/%s/users", c.apiBase, c.envID)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create user request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create user HTTP %d: %s", resp.StatusCode, body)
	}

	var u PingOneUser
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("decode create user response: %w", err)
	}
	return &u, nil
}

// parsePingOneCoords derives the PingOne env ID and management API base URL
// from an issuer URL of the form https://auth.pingone.<region>/<env-id>/as
func parsePingOneCoords(issuer string) (envID, apiBase string, err error) {
	withoutScheme := strings.TrimPrefix(issuer, "https://")
	parts := strings.SplitN(withoutScheme, "/", 4)
	if len(parts) < 2 || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse env ID from %q", issuer)
	}
	envID = parts[1]
	apiHost := strings.Replace(parts[0], "auth.", "api.", 1)
	apiBase = "https://" + apiHost + "/v1"
	return envID, apiBase, nil
}
