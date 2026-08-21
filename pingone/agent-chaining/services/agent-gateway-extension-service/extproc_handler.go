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
	validator                                                  *delegatedTokenValidator
}

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer
	targets []targetConfig
	idp     *idpClient
	authz   *pingoneAuthorizeClient
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
	mcp, err := parseTarget(cfg.mcpURL, "MCP", "mcp", cfg.mcpAudience, cfg.mcpScope, cfg.mcpActor)
	if err != nil {
		return nil, err
	}
	if cfg.authzMode != "permit-all" && (cfg.authzEndpoint == "" || cfg.authzClientID == "" || cfg.authzClientSecret == "") {
		return nil, fmt.Errorf("PingOne Authorize configuration is required unless AUTHZ_MODE=permit-all")
	}
	return &shim{targets: []targetConfig{a2a, mcp}, idp: newIDPClient(cfg.idpEndpoint, cfg.idpClientID, cfg.idpSecret), authz: &pingoneAuthorizeClient{}}, nil
}

func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var target *targetConfig
	var subject, actor string
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
			resp, target, subject, actor = s.onHeaders(stream.Context(), v.RequestHeaders)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onBody(v, target, subject, actor)
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

func (s *shim) onHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (*extprocv3.ProcessingResponse, *targetConfig, string, string) {
	authority, path := headerValue(msg.Headers, ":authority"), headerValue(msg.Headers, ":path")
	target := s.targetFor(authority, path)
	if target == nil {
		return passthroughHeaders(), nil, "", ""
	}
	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		return denyUnauthorized("bearer token required"), target, "", ""
	}
	if err := target.validator.verify(ctx, bearer); err != nil {
		return denyUnauthorized("invalid delegated token"), target, "", ""
	}
	subject := jwtClaim(bearer, "sub")
	// Plumbing mode intentionally validates only audience and scope. Actor
	// enforcement is added with the real authorization policy in the next phase.
	actor := "delegated-agent"
	tok, err := s.idp.exchangeForTarget(bearer, target.name, target.audience, target.scope)
	if err != nil {
		return denyForbidden("token exchange failed"), target, "", ""
	}
	log.Printf("[ExtSvc] target=%s protocol=%s subject=%s actor=%s", target.name, target.protocol, subject, actor)
	return injectAuthAndRequestBody(tok), target, subject, actor
}

func (s *shim) onBody(b *extprocv3.ProcessingRequest_RequestBody, target *targetConfig, subject, actor string) *extprocv3.ProcessingResponse {
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
	return echoRequestBody(b.RequestBody)
}

func parseRequest(protocol string, body []byte) (string, string, bool) {
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return "", "", false
	}
	if protocol == "a2a" && req["method"] == "message/send" {
		return "get_order_status", "", true
	}
	if protocol == "mcp" && req["method"] == "tools/call" {
		params, _ := req["params"].(map[string]any)
		if params["name"] == "get_order_status" {
			return "get_order_status", "", true
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
