// AuthN/AuthZ Extension Service — deployed on Cloud Run.
//
// This is the ext_proc handler that the real GCP Agent Gateway calls via gRPC
// Service Extensions on every inbound request. It acts as the Policy Decision
// Point (PDP) bridge to PingOne.
//
// Responsibilities:
//  1. Validate the agent's identity claims from the request headers
//     (the Agent Gateway populates x-agent-id with the workload's identity).
//  2. Authorize the agent against a policy store (here: an in-memory map;
//     in production: Open Policy Agent or the IdP's authorization server).
//  3. Perform an OAuth 2.0 Token Exchange (RFC 8693) with PingOne,
//     trading the verified agent context for a scoped access token.
//  4. Return a ProcessingResponse telling the gateway to Permit (with the
//     token injected as a header mutation) or Deny (with a 403 ImmediateResponse).
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

// allowedAgents maps agent identity (from x-agent-id header) → permitted scopes.
// In production, back this with Open Policy Agent or your IdP's authorization API.
var allowedAgents = map[string][]string{
	"crm-agent@PROJECT_ID.iam.gserviceaccount.com": {"supply-chain:restock"},
}

type server struct {
	extprocv3.UnimplementedExternalProcessorServer
	idpTokenEndpoint string
	idpClientID      string
	idpClientSecret  string
	idpAudience      string
	httpClient       *http.Client
}

func main() {
	// Load a local .env if present (for `go run` on a laptop). Absent in Cloud
	// Run, where env vars come from the service definition — so ignore the error.
	_ = godotenv.Load()

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	srv := &server{
		idpTokenEndpoint: mustEnv("IDP_TOKEN_ENDPOINT"),
		idpClientID:      mustEnv("IDP_CLIENT_ID"),
		idpClientSecret:  mustEnv("IDP_CLIENT_SECRET"),
		idpAudience:      os.Getenv("IDP_AUDIENCE"),
		httpClient:       &http.Client{Timeout: 5 * time.Second},
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(grpcServer, srv)
	reflection.Register(grpcServer)

	log.Printf("[ExtSvc] Listening on :%s (gRPC)", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// Process is the single streaming RPC the Agent Gateway calls for every request.
// The gateway sends a stream of ProcessingRequest messages; we respond to the
// RequestHeaders phase and let everything else pass through.
func (s *server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv: %v", err)
		}

		// We only act on the request headers phase.
		headers, ok := req.Request.(*extprocv3.ProcessingRequest_RequestHeaders)
		if !ok {
			// For all other phases (body, response headers, etc.), pass through.
			if err := stream.Send(&extprocv3.ProcessingResponse{}); err != nil {
				return err
			}
			continue
		}

		resp, err := s.handleRequestHeaders(stream.Context(), headers.RequestHeaders)
		if err != nil {
			return err
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

func (s *server) handleRequestHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (*extprocv3.ProcessingResponse, error) {
	agentID := headerValue(msg.Headers, "x-agent-id")
	path := headerValue(msg.Headers, ":path")

	log.Printf("[ExtSvc] Authorize — agent: %s, path: %s", agentID, path)

	scopes, permitted := allowedAgents[agentID]
	if !permitted {
		log.Printf("[ExtSvc] DENY — agent not in policy store: %s", agentID)
		return denyResponse(403, "agent not authorized"), nil
	}

	token, err := s.exchangeToken(agentID, scopes)
	if err != nil {
		log.Printf("[ExtSvc] Token exchange failed for %s: %v", agentID, err)
		return denyResponse(503, "token exchange failed"), nil
	}

	log.Printf("[ExtSvc] PERMIT — injecting OAuth token (scopes: %s)", strings.Join(scopes, " "))
	return permitResponse(token, agentID), nil
}

// permitResponse tells the gateway to forward the request, enriched with the
// OAuth token and the agent's identity header for downstream visibility.
func permitResponse(token, agentID string) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders: []*corev3.HeaderValueOption{
							{Header: &corev3.HeaderValue{Key: "Authorization", Value: "Bearer " + token}},
							{Header: &corev3.HeaderValue{Key: "X-Spiffe-Id", Value: agentID}},
						},
					},
				},
			},
		},
	}
}

// denyResponse tells the gateway to return an HTTP error immediately, before
// the request ever reaches the supply chain tool.
func denyResponse(httpStatus int32, body string) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status: &typev3.HttpStatus{Code: typev3.StatusCode(httpStatus)},
				Body:   body,
			},
		},
	}
}

// exchangeToken performs RFC 8693 Token Exchange: trades the verified agent
// identity for a scoped OAuth 2.0 access token from PingOne.
func (s *server) exchangeToken(subjectIdentity string, scopes []string) (string, error) {
	form := strings.NewReader(strings.Join([]string{
		"grant_type=urn:ietf:params:oauth:grant-type:token-exchange",
		"client_id=" + s.idpClientID,
		"client_secret=" + s.idpClientSecret,
		"subject_token=" + subjectIdentity,
		"subject_token_type=urn:ietf:params:oauth:token-type:jwt",
		"requested_token_type=urn:ietf:params:oauth:token-type:access_token",
		"scope=" + strings.Join(scopes, " "),
		"audience=" + s.idpAudience,
	}, "&"))

	resp, err := s.httpClient.Post(s.idpTokenEndpoint, "application/x-www-form-urlencoded", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", &tokenError{result.Error, result.Description}
	}
	return result.AccessToken, nil
}

func headerValue(headers *corev3.HeaderMap, key string) string {
	if headers == nil {
		return ""
	}
	for _, h := range headers.Headers {
		if strings.EqualFold(h.Key, key) {
			return h.Value
		}
	}
	return ""
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

type tokenError struct{ code, desc string }

func (e *tokenError) Error() string { return e.code + ": " + e.desc }
