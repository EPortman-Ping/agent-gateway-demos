package main

import (
	"context"
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

	toolURL        string
	idp            *idpClient
	authz          *pingoneAuthorizeClient
	userResolver   *pingoneUserResolver
	tokenValidator *tokenValidator
}

type shimConfig struct {
	toolURL           string
	idpEndpoint       string
	idpClientID       string
	idpSecret         string
	idpScope          string
	idpAudience       string
	authzEndpoint     string
	authzClientID     string
	authzClientSecret string
}

func newShim(cfg shimConfig) *shim {
	s := &shim{
		toolURL: cfg.toolURL,
		idp: &idpClient{
			endpoint:     cfg.idpEndpoint,
			clientID:     cfg.idpClientID,
			clientSecret: cfg.idpSecret,
			scope:        cfg.idpScope,
		},
	}

	if cfg.idpAudience != "" {
		ctx := context.Background()
		if v, err := newTokenValidator(ctx, cfg.idpEndpoint, cfg.idpAudience, cfg.idpScope); err != nil {
			log.Printf("[ExtSvc] WARNING: token validator init failed: %v — inbound token validation disabled", err)
		} else {
			s.tokenValidator = v
		}
	} else {
		log.Println("[ExtSvc] WARNING: IDP_REQUIRED_AUDIENCE not set — inbound token validation disabled")
	}

	// Derive PingOne env ID and API base from the token endpoint.
	// IDP_TOKEN_ENDPOINT form: https://auth.pingone.<region>/<env-id>/as/token
	envID, apiBase, err := parsePingOneCoords(cfg.idpEndpoint)
	if err != nil {
		log.Printf("[ExtSvc] WARNING: cannot derive PingOne coords from %q: %v — user email injection disabled", cfg.idpEndpoint, err)
	}
	if envID != "" && apiBase != "" {
		s.userResolver = &pingoneUserResolver{
			envID:         envID,
			apiBase:       apiBase,
			tokenEndpoint: cfg.idpEndpoint,
			clientID:      cfg.authzClientID,
			clientSecret:  cfg.authzClientSecret,
		}
		log.Printf("[ExtSvc] user email resolver enabled (envID=%s)", envID)
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
//  1. Header phase: validate the inbound PingOne user token (minted by the
//     Gemini Enterprise auth manager), exchange it for a tool-scoped token,
//     resolve the user's email, inject headers, and request the body.
//  2. Body phase: extract the MCP method. On tools/call: call PingOne Authorize
//     with user sub, tool name, department, and current hour.
func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var userSub string
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
			resp, userSub, needsAuthz = s.onRequestHeaders(stream.Context(), v.RequestHeaders)

		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onRequestBody(v, userSub, needsAuthz)

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

// onRequestHeaders validates the inbound PingOne user token issued by the
// Gemini Enterprise auth manager, exchanges it for a tool-scoped token,
// resolves the user's email, and requests the body for the Authorize check.
func (s *shim) onRequestHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (resp *extprocv3.ProcessingResponse, userSub string, needsAuthz bool) {
	authority := headerValue(msg.Headers, ":authority")
	path := headerValue(msg.Headers, ":path")

	if !s.configured() || !s.isToolRequest(authority) {
		return passthroughHeaders(), "", false
	}

	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		log.Printf("[ExtSvc] missing bearer token — 401")
		return denyUnauthorized("bearer token required"), "", false
	}

	if s.tokenValidator != nil {
		if err := s.tokenValidator.verify(ctx, bearer); err != nil {
			log.Printf("[ExtSvc] token validation failed — 401: %v", err)
			return denyUnauthorized("invalid token: " + err.Error()), "", false
		}
	}

	userSub = jwtClaim(bearer, "sub")

	tok, err := s.idp.exchangeForTool(bearer)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return denyForbidden("token exchange failed"), "", false
	}

	// Resolve the user's email from their sub and inject it as X-User-Email so
	// the MCP server can identify the caller without needing PingOne access.
	userEmail := ""
	if s.userResolver != nil && userSub != "" {
		if email, err := s.userResolver.emailForSub(userSub); err != nil {
			log.Printf("[ExtSvc] WARNING: email lookup failed for sub=%s: %v — proceeding without email", userSub, err)
		} else {
			userEmail = email
		}
	}

	serviceName := strings.SplitN(authority, ".", 2)[0]
	log.Printf("[ExtSvc] %s %s — user=%s email=%s", serviceName, path, userSub, userEmail)
	return injectAuthAndEmailAndRequestBody(tok, userEmail), userSub, s.authz != nil
}

// onRequestBody handles the body phase. For tools/call it calls PingOne
// Authorize with user sub, tool name, and current hour.
func (s *shim) onRequestBody(b *extprocv3.ProcessingRequest_RequestBody, userSub string, needsAuthz bool) *extprocv3.ProcessingResponse {
	body := b.RequestBody.Body
	if !needsAuthz || extractMethod(body) != "tools/call" {
		return echoRequestBody(b.RequestBody)
	}

	toolName := extractToolName(body)
	log.Printf("[ExtSvc] authorize user=%s tool=%s hour=%d",
		userSub, toolName, currentHour())

	permitted, err := s.authz.Decide(userSub, toolName, currentHour())
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return denyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY user=%s", userSub)
		return denyForbidden("request denied by policy")
	default:
		log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s", userSub)
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

func currentHour() int {
	return time.Now().UTC().Hour()
}
