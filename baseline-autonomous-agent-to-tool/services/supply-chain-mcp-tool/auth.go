// Token validation — the security boundary of this service.
//
// The Agent Gateway injects a scoped OAuth 2.0 access token, but this service
// does NOT blindly trust that. It independently validates the token —
// signature (against PingOne's JWKS), issuer, audience, expiry, and the
// required scope — before any request reaches the MCP handler. Defense in
// depth: even if a request somehow bypasses the gateway, an unsigned or
// wrong-scope token is rejected here.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// tokenValidator independently verifies the OAuth access token the gateway
// injected. It caches PingOne's JWKS (refreshed automatically) and checks
// signature, issuer, audience, expiry, and the required scope on every request.
type tokenValidator struct {
	issuer        string
	audience      string
	jwksURL       string
	requiredScope string
	keys          *jwk.Cache
}

// newTokenValidator wires the validator from env vars. If IDP_ISSUER is unset
// (e.g. local dev with no IdP), it returns a validator in "unverified" mode
// that only checks a token is present — with a loud warning, so this can never
// be mistaken for real validation in a deployed environment.
func newTokenValidator(ctx context.Context) (*tokenValidator, error) {
	issuer := os.Getenv("IDP_ISSUER")
	audience := os.Getenv("IDP_AUDIENCE")
	jwksURL := os.Getenv("IDP_JWKS_URL")
	requiredScope := os.Getenv("IDP_REQUIRED_SCOPE")

	if issuer == "" {
		log.Printf("[SupplyChain] WARNING: IDP_ISSUER unset — token signature/claims are NOT verified (presence-only). Set IDP_ISSUER, IDP_AUDIENCE, IDP_JWKS_URL, IDP_REQUIRED_SCOPE for real validation.")
		return &tokenValidator{}, nil
	}
	if audience == "" {
		return nil, fmt.Errorf("IDP_AUDIENCE is required when IDP_ISSUER is set")
	}
	if requiredScope == "" {
		return nil, fmt.Errorf("IDP_REQUIRED_SCOPE is required when IDP_ISSUER is set")
	}
	if jwksURL == "" {
		return nil, fmt.Errorf("IDP_JWKS_URL is required when IDP_ISSUER is set")
	}

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS url: %w", err)
	}
	// Warm the cache so a bad URL fails fast at startup rather than per-request.
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[SupplyChain] Token validation ON — issuer=%s audience=%s scope=%s jwks=%s", issuer, audience, requiredScope, jwksURL)
	return &tokenValidator{issuer: issuer, audience: audience, jwksURL: jwksURL, requiredScope: requiredScope, keys: cache}, nil
}

// middleware validates the injected token before the request reaches the MCP
// handler. On any failure it rejects with 401/403 so the tool logic never runs.
func (v *tokenValidator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spiffeID := r.Header.Get("X-Spiffe-Id") // injected by the Agent Gateway
		log.Printf("[SupplyChain] Received %s %s (X-Spiffe-Id: %s)", r.Method, r.URL.Path, spiffeID)

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("[SupplyChain] REJECT — no Bearer token (request did not come through the gateway)")
			http.Error(w, "missing Bearer token", http.StatusUnauthorized)
			return
		}
		raw := strings.TrimPrefix(authHeader, "Bearer ")

		if err := v.verify(r.Context(), raw); err != nil {
			log.Printf("[SupplyChain] REJECT — token validation failed: %v", err)
			http.Error(w, "invalid token: "+err.Error(), http.StatusForbidden)
			return
		}

		log.Printf("[SupplyChain] Token verified — scope %q present, forwarding to MCP handler", v.requiredScope)
		next.ServeHTTP(w, r)
	})
}

// verify checks the token's signature, issuer, audience, expiry, and scope.
// In unverified mode (no IdP configured) it only confirms a token is present.
func (v *tokenValidator) verify(ctx context.Context, raw string) error {
	if v.keys == nil {
		if raw == "" {
			return fmt.Errorf("empty token")
		}
		log.Printf("[SupplyChain] (unverified mode) accepting token first 40 chars: %s...", truncate(raw, 40))
		return nil
	}

	set, err := v.keys.Get(ctx, v.jwksURL)
	if err != nil {
		return fmt.Errorf("load JWKS: %w", err)
	}

	// jwt.Parse verifies the signature against the key set and enforces exp/nbf.
	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(set),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return err
	}

	if !hasScope(tok, v.requiredScope) {
		return fmt.Errorf("missing required scope %q", v.requiredScope)
	}
	return nil
}

// hasScope reports whether the token carries the given OAuth scope. PingOne
// issues the space-delimited "scope" string claim; the "scp" array form is also
// accepted for robustness.
func hasScope(tok jwt.Token, want string) bool {
	if raw, ok := tok.Get("scope"); ok {
		if s, ok := raw.(string); ok && slices.Contains(strings.Fields(s), want) {
			return true
		}
	}
	if raw, ok := tok.Get("scp"); ok {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && s == want {
					return true
				}
			}
		}
	}
	return false
}

// truncate returns at most the first n bytes of s (used for safe token logging).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
