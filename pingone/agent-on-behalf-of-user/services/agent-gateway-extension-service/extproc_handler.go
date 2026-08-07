package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer

	toolURL         string
	stepUpThreshold int    // USD cents; step-up triggers above this
	stepUpScope     string // scope that satisfies step-up (e.g. stripe_mcp:high_value)
	idp             *idpClient
	authz           *pingoneAuthorizeClient
}

type shimConfig struct {
	toolURL           string
	stepUpThreshold   int
	stepUpScope       string
	idpEndpoint       string
	idpClientID       string
	idpSecret         string
	idpScope          string
	authzEndpoint     string
	authzClientID     string
	authzClientSecret string
}

func newShim(cfg shimConfig) *shim {
	s := &shim{
		toolURL:         cfg.toolURL,
		stepUpThreshold: cfg.stepUpThreshold,
		stepUpScope:     cfg.stepUpScope,
		idp: &idpClient{
			endpoint:     cfg.idpEndpoint,
			clientID:     cfg.idpClientID,
			clientSecret: cfg.idpSecret,
			scope:        cfg.idpScope,
		},
	}
	if cfg.authzEndpoint != "" {
		s.authz = &pingoneAuthorizeClient{
			decisionEndpoint: cfg.authzEndpoint,
			tokenEndpoint:    cfg.idpEndpoint,
			clientID:         cfg.authzClientID,
			clientSecret:     cfg.authzClientSecret,
		}
		log.Printf("[ExtSvc] PingOne Authorize enabled: %s", cfg.authzEndpoint)
	} else {
		log.Println("[ExtSvc] WARNING: AUTHZ_DECISION_ENDPOINT not set — skipping PingOne Authorize check")
	}
	if !s.configured() {
		log.Println("[ExtSvc] WARNING: TOOL_URL / IDP_TOKEN_ENDPOINT / IDP_CLIENT_ID / IDP_CLIENT_SECRET incomplete — tool requests will be denied")
	}
	return s
}

func (s *shim) configured() bool {
	return s.toolURL != "" && s.idp.endpoint != "" && s.idp.clientID != "" && s.idp.clientSecret != ""
}

// Process is the ext_proc stream handler.
//
// Per-request flow (two phases):
//  1. Header phase: validate the delegated bearer token (must carry sub + act
//     claims), exchange it for a tool-scoped delegated token, inject it, and
//     request the body (BUFFERED) for the Authorize check.
//  2. Body phase: extract the MCP method and payment amount. On tools/call:
//       - if amount > step-up threshold → 401 step_up_required
//       - otherwise call PingOne Authorize with compound attributes (user sub +
//         agent act.client_id + amount), echo body on PERMIT or deny on DENY.
func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var userSub string
	var agentClientID string
	var tokenScope string
	var needsAuthz bool

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv: %v", err)
		}

		var resp *extprocv3.ProcessingResponse
		switch v := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp, userSub, agentClientID, tokenScope, needsAuthz = s.onRequestHeaders(v.RequestHeaders)

		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onRequestBody(v, userSub, agentClientID, tokenScope, needsAuthz)

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp = ackResponseHeaders()
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp = echoResponseBody(v.ResponseBody)
		default:
			resp = &extprocv3.ProcessingResponse{}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// onRequestHeaders validates the incoming delegated bearer token. The token
// must carry both a sub claim (the human user) and an act.client_id claim (the
// agent). On success it exchanges the token for a tool-scoped token, injects
// it, and requests the body for the downstream Authorize + step-up check.
func (s *shim) onRequestHeaders(msg *extprocv3.HttpHeaders) (resp *extprocv3.ProcessingResponse, userSub, agentClientID, tokenScope string, needsAuthz bool) {
	authority := headerValue(msg.Headers, ":authority")
	path := headerValue(msg.Headers, ":path")
	log.Printf("[ExtSvc] request authority=%q path=%q", authority, path)

	if !s.configured() || !s.isToolRequest(authority) {
		return passthroughHeaders(), "", "", "", false
	}

	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		log.Printf("[ExtSvc] missing bearer token — 401")
		return denyUnauthorized("bearer token required"), "", "", "", false
	}

	userSub = jwtClaim(bearer, "sub")
	agentClientID = jwtActClaim(bearer)
	tokenScope = jwtClaim(bearer, "scope")
	if userSub == "" || agentClientID == "" {
		log.Printf("[ExtSvc] token missing sub or act.client_id — 401 (sub=%q act.client_id=%q)", userSub, agentClientID)
		return denyUnauthorized("token must carry sub and act.client_id claims"), "", "", "", false
	}

	tok, err := s.idp.exchangeForTool(bearer)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return denyForbidden("token exchange failed"), "", "", "", false
	}

	log.Printf("[ExtSvc] injecting delegated tool token for %s (user=%s agent=%s scope=%q)", authority, userSub, agentClientID, tokenScope)
	return injectAuthAndRequestBody(tok), userSub, agentClientID, tokenScope, s.authz != nil
}

// onRequestBody handles the body phase. For tools/call it checks for step-up
// and then calls PingOne Authorize with compound attributes.
func (s *shim) onRequestBody(b *extprocv3.ProcessingRequest_RequestBody, userSub, agentClientID, tokenScope string, needsAuthz bool) *extprocv3.ProcessingResponse {
	body := b.RequestBody.Body
	if !needsAuthz || extractMethod(body) != "tools/call" {
		return echoRequestBody(b.RequestBody)
	}

	amountCents := extractTotalPriceCents(body)
	toolName := extractToolName(body)
	log.Printf("[ExtSvc] authorize user=%s agent=%s tool=%s amount_cents=%d hour=%d",
		userSub, agentClientID, toolName, amountCents, currentHour())

	// Step-up: high-value transactions require the elevated scope. If the token
	// already carries it the user has completed MFA — proceed to Authorize.
	if s.stepUpThreshold > 0 && amountCents > s.stepUpThreshold && !hasScope(tokenScope, s.stepUpScope) {
		log.Printf("[ExtSvc] step-up required: amount_cents=%d > threshold=%d and scope %q absent", amountCents, s.stepUpThreshold, s.stepUpScope)
		return stepUpRequired(amountCents, s.stepUpThreshold)
	}

	permitted, err := s.authz.Decide(userSub, agentClientID, toolName, amountCents, currentHour())
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return denyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY user=%s agent=%s", userSub, agentClientID)
		return denyForbidden("request denied by policy")
	default:
		log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s agent=%s", userSub, agentClientID)
		return echoRequestBody(b.RequestBody)
	}
}

func (s *shim) isToolRequest(authority string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(s.toolURL, "https://"), "http://")
	return strings.HasPrefix(authority, host)
}

func headerValue(headers *corev3.HeaderMap, key string) string {
	if headers == nil {
		return ""
	}
	for _, h := range headers.Headers {
		if strings.EqualFold(h.Key, key) {
			if h.Value != "" {
				return h.Value
			}
			return string(h.RawValue)
		}
	}
	return ""
}

// jwtClaim extracts a top-level string claim from a JWT without signature
// validation. Used only for logging and Authorize attributes.
func jwtClaim(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	v, _ := claims[claim].(string)
	return v
}

// jwtActClaim extracts act.client_id from the JWT's act object.
func jwtActClaim(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Act struct {
			ClientID string `json:"client_id"`
		} `json:"act"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Act.ClientID
}

func extractMethod(body []byte) string {
	var rpc struct {
		Method string `json:"method"`
	}
	json.Unmarshal(body, &rpc) //nolint:errcheck
	return rpc.Method
}

func extractToolName(body []byte) string {
	var rpc struct {
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	json.Unmarshal(body, &rpc) //nolint:errcheck
	return rpc.Params.Name
}

// extractTotalPriceCents extracts the total_price argument (USD) from a
// create_stripe_payment_intent call and converts to cents for comparison.
func extractTotalPriceCents(body []byte) int {
	var rpc struct {
		Params struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return 0
	}
	switch v := rpc.Params.Arguments["total_price"].(type) {
	case float64:
		return int(v * 100)
	}
	return 0
}

func currentHour() int {
	return time.Now().UTC().Hour()
}

// hasScope reports whether the space-separated scope string contains target.
// If target is empty, it always returns true (step-up not configured).
func hasScope(scopeStr, target string) bool {
	if target == "" {
		return true
	}
	for _, s := range strings.Fields(scopeStr) {
		if s == target {
			return true
		}
	}
	return false
}
