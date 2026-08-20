package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer

	toolURL        string
	tokenValidator *tokenValidator
}

type shimConfig struct {
	toolURL     string
	idpEndpoint string
	idpAudience string
	idpScope    string
}

func newShim(cfg shimConfig) *shim {
	s := &shim{toolURL: cfg.toolURL}

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

	log.Println("[ExtSvc] PingOne Authorize: DISABLED (approve-all mode)")
	if cfg.toolURL == "" {
		log.Println("[ExtSvc] WARNING: TOOL_URL not set — all requests will pass through")
	}
	return s
}

func (s *shim) configured() bool {
	return s.toolURL != ""
}

// Process is the ext_proc stream handler.
//
// Per-request flow (two phases):
//  1. Header phase: validate the inbound PingOne user token (minted by the
//     Gemini Enterprise auth manager), resolve the user's email, inject
//     X-User-Email, and request the body.
//  2. Body phase: echo the body through. All tool calls are approved.
//     (PingOne Authorize integration is the next phase.)
func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var userSub string

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
			resp, userSub = s.onRequestHeaders(stream.Context(), v.RequestHeaders)

		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onRequestBody(v, userSub)

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
// Gemini Enterprise auth manager and requests the body. The bearer token is
// passed through unchanged — token exchange will be wired in when PingOne
// Authorize is added in the next phase.
func (s *shim) onRequestHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (resp *extprocv3.ProcessingResponse, userSub string) {
	authority := headerValue(msg.Headers, ":authority")
	path := headerValue(msg.Headers, ":path")

	if !s.configured() || !s.isToolRequest(authority) {
		return passthroughHeaders(), ""
	}

	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		// No token on initialize/tools/list — pass through, MCP server handles it.
		return requestBody(), ""
	}

	if s.tokenValidator != nil {
		if err := s.tokenValidator.verify(ctx, bearer); err != nil {
			log.Printf("[ExtSvc] token validation failed — 401: %v", err)
			return denyUnauthorized("invalid token: " + err.Error()), ""
		}
	}

	userSub = jwtClaim(bearer, "sub")
	serviceName := strings.SplitN(authority, ".", 2)[0]
	log.Printf("[ExtSvc] %s %s — user=%s", serviceName, path, userSub)
	return requestBody(), userSub
}

// onRequestBody logs tool calls and echoes the body through. All requests are
// approved — PingOne Authorize integration is the next phase.
func (s *shim) onRequestBody(b *extprocv3.ProcessingRequest_RequestBody, userSub string) *extprocv3.ProcessingResponse {
	body := b.RequestBody.Body
	if extractMethod(body) == "tools/call" {
		toolName := extractToolName(body)
		log.Printf("[ExtSvc] APPROVE (no-authz mode) user=%s tool=%s", userSub, toolName)
	}
	return echoRequestBody(b.RequestBody)
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

