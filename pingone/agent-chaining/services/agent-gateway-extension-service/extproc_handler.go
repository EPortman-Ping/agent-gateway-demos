package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type targetConfig struct {
	name, host, path, audience, scope, expectedActor, protocol string
	// dualAuth marks targets that are themselves Google-hosted API surfaces
	// (e.g. aiplatform.googleapis.com reasoning engines). Those enforce their
	// own Google IAM check on Authorization independent of gateway policy, so
	// the outer call needs a Google credential in addition to the PingOne
	// delegated token the downstream agent validates.
	dualAuth  bool
	validator *delegatedTokenValidator
}

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer
	targets    []targetConfig
	idp        *idpClient
	authz      *pingoneAuthorizeClient
	googleAuth *googleTokenSource
}

type shimConfig struct {
	a2aURL, a2aAudience, a2aScope, a2aActor                    string
	mcpURL, mcpAudience, mcpScope, mcpActor                    string
	idpEndpoint, idpClientID, idpSecret                        string
	authzEndpoint, authzClientID, authzClientSecret, authzMode string
}

func newShim(cfg shimConfig) (*shim, error) {
	parseTarget := func(raw, name, protocol, audience, scope, actor string) (targetConfig, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return targetConfig{}, fmt.Errorf("invalid %s target URL", name)
		}
		validator, err := newDelegatedTokenValidator(context.Background(), cfg.idpEndpoint, audience, scope)
		if err != nil {
			return targetConfig{}, err
		}
		return targetConfig{name: name, host: u.Host, path: u.Path, audience: audience, scope: scope, expectedActor: actor, protocol: protocol, validator: validator}, nil
	}
	a2a, err := parseTarget(cfg.a2aURL, "A2A", "a2a", cfg.a2aAudience, cfg.a2aScope, cfg.a2aActor)
	if err != nil {
		return nil, err
	}
	a2a.dualAuth = true
	mcp, err := parseTarget(cfg.mcpURL, "MCP", "mcp", cfg.mcpAudience, cfg.mcpScope, cfg.mcpActor)
	if err != nil {
		return nil, err
	}
	if cfg.authzMode != "permit-all" && (cfg.authzEndpoint == "" || cfg.authzClientID == "" || cfg.authzClientSecret == "") {
		return nil, fmt.Errorf("PingOne Authorize configuration is required unless AUTHZ_MODE=permit-all")
	}
	googleAuth, err := newGoogleTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("google credentials: %w", err)
	}
	return &shim{
		targets:    []targetConfig{a2a, mcp},
		idp:        newIDPClient(cfg.idpEndpoint, cfg.idpClientID, cfg.idpSecret),
		authz:      &pingoneAuthorizeClient{},
		googleAuth: googleAuth,
	}, nil
}

func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var target *targetConfig
	var subject, actor, bodyToken string
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
			resp, target, subject, actor, bodyToken = s.onHeaders(stream.Context(), v.RequestHeaders)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onBody(v, target, subject, actor, bodyToken)
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

func (s *shim) targetFor(authority, path string) *targetConfig {
	for i := range s.targets {
		if s.targets[i].host == authority && strings.HasPrefix(path, s.targets[i].path) {
			return &s.targets[i]
		}
	}
	return nil
}

// onHeaders validates the caller's delegated bearer, remints a fresh
// PingOne-delegated token audienced to this hop's target (adding the
// gateway's own actor, mirroring the MCP hop's existing exchange), and for
// Google-hosted targets also injects a Google credential to satisfy that
// API's own IAM check. It returns the reminted token separately (bodyToken)
// so onBody can place it in the request body for dualAuth targets, since a
// custom header added here was observed not to reach the downstream agent.
func (s *shim) onHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (*extprocv3.ProcessingResponse, *targetConfig, string, string, string) {
	authority, path := headerValue(msg.Headers, ":authority"), headerValue(msg.Headers, ":path")
	target := s.targetFor(authority, path)
	log.Printf("[ExtSvc] onHeaders authority=%q path=%q matched=%v", authority, path, target != nil)
	if target == nil {
		return passthroughHeaders(), nil, "", "", ""
	}
	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		return denyUnauthorized("bearer token required"), target, "", "", ""
	}
	if err := target.validator.verify(ctx, bearer); err != nil {
		return denyUnauthorized("invalid delegated token"), target, "", "", ""
	}
	subject := jwtClaim(bearer, "sub")
	// Plumbing mode intentionally validates only audience and scope. Actor
	// enforcement is added with the real authorization policy in the next phase.
	actor := "delegated-agent"

	reminted, err := s.idp.exchangeForTarget(bearer, target.name, target.audience, target.scope)
	if err != nil {
		log.Printf("[ExtSvc] target=%s token exchange failed: %v", target.name, err)
		return denyForbidden("token exchange failed"), target, "", "", ""
	}

	if target.dualAuth {
		googleTok, err := s.googleAuth.Token()
		if err != nil {
			log.Printf("[ExtSvc] target=%s google credential error: %v", target.name, err)
			return denyForbidden("upstream credential error"), target, "", "", ""
		}
		log.Printf("[ExtSvc] target=%s protocol=%s subject=%s actor=%s (google-auth+remint)", target.name, target.protocol, subject, actor)
		return injectGoogleAuth(googleTok), target, subject, actor, reminted
	}
	log.Printf("[ExtSvc] target=%s protocol=%s subject=%s actor=%s", target.name, target.protocol, subject, actor)
	return injectAuthAndRequestBody(reminted), target, subject, actor, ""
}

func (s *shim) onBody(b *extprocv3.ProcessingRequest_RequestBody, target *targetConfig, subject, actor, bodyToken string) *extprocv3.ProcessingResponse {
	if target == nil {
		return echoRequestBody(b.RequestBody)
	}
	body := b.RequestBody.Body
	action, orderID, ok := parseRequest(target.protocol, body)
	if !ok {
		return denyForbidden("unsupported request")
	}
	permitted, err := s.authz.Decide(subject, actor, action, 0, currentHour())
	if err != nil || !permitted {
		return denyForbidden("request denied by policy")
	}
	log.Printf("[ExtSvc] PERMIT target=%s action=%s order=%s", target.name, action, orderID)
	if bodyToken != "" {
		mutated, err := setDelegatedAuthorizationInBody(body, bodyToken)
		if err != nil {
			log.Printf("[ExtSvc] target=%s body mutation error: %v", target.name, err)
			return denyForbidden("request body error")
		}
		return replaceRequestBody(mutated, b.RequestBody.EndOfStream)
	}
	return echoRequestBody(b.RequestBody)
}

// setDelegatedAuthorizationInBody replaces the message's delegatedAuthorization
// metadata with the gateway-reminted token, so the downstream agent validates
// the token the gateway just exchanged rather than the caller's original one.
func setDelegatedAuthorizationInBody(body []byte, token string) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	metadata, _ := req["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["delegatedAuthorization"] = "Bearer " + token
	req["metadata"] = metadata
	return json.Marshal(req)
}

func parseRequest(protocol string, body []byte) (string, string, bool) {
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return "", "", false
	}
	if protocol == "a2a" {
		message, _ := req["message"].(map[string]any)
		parts, _ := message["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			text, _ := part["text"].(string)
			if strings.HasPrefix(text, "get_order_status:") {
				orderID := strings.TrimPrefix(text, "get_order_status:")
				if strings.HasPrefix(orderID, "ORD-") && len(orderID) > 4 {
					return "get_order_status", orderID, true
				}
			}
		}
		return "", "", false
	}
	if protocol == "mcp" && req["method"] == "tools/call" {
		params, _ := req["params"].(map[string]any)
		if params["name"] == "get_order_status" {
			arguments, _ := params["arguments"].(map[string]any)
			orderID, _ := arguments["order_id"].(string)
			return "get_order_status", orderID, orderID != ""
		}
	}
	return "", "", false
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
func jwtClaim(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var c map[string]any
	if json.Unmarshal(payload, &c) != nil {
		return ""
	}
	v, _ := c[claim].(string)
	return v
}
func currentHour() int { return time.Now().Hour() }
