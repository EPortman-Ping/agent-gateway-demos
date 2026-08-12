// Agent Gateway extension service — an Envoy ext_proc gRPC handler.
//
// The Agent Gateway calls this service for every MCP request egressing from a
// Gemini Enterprise app. The inbound bearer token is a PingOne user token
// minted by the Gemini Enterprise auth manager's 3-legged OAuth flow.
//
// For requests bound to the HR MCP tool this service:
//  1. Validates the inbound user token (iss, aud, scope, sig via JWKS).
//  2. Resolves the user's email from their sub via the PingOne management API.
//  3. On tools/call: calls PingOne Authorize with user_sub, tool_name,
//     department, and request_hour. Denies if DENY or INDETERMINATE.
//  4. On PERMIT: performs an RFC 8693 exchange — the extension service's own
//     client acts as actor — minting a token audienced to the HR MCP server.
//  5. Injects the tool token and X-User-Email header before forwarding.
//
// It fails closed: unauthorized requests get an immediate error and never
// reach the tool. Every other request passes through untouched.
//
// Cloud Run terminates TLS, so we serve plain h2c on GRPC_PORT.
package main

import (
	"log"
	"net"
	"os"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func main() {
	_ = godotenv.Load() // local dev convenience; absent on Cloud Run

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	shim := newShim(shimConfig{
		toolURL:           os.Getenv("TOOL_URL"),
		idpEndpoint:       os.Getenv("IDP_TOKEN_ENDPOINT"),
		idpClientID:       os.Getenv("IDP_CLIENT_ID"),
		idpSecret:         os.Getenv("IDP_CLIENT_SECRET"),
		idpScope:          os.Getenv("IDP_SCOPE"),
		idpAudience:       os.Getenv("IDP_REQUIRED_AUDIENCE"),
		authzEndpoint:     os.Getenv("AUTHZ_DECISION_ENDPOINT"),
		authzClientID:     os.Getenv("AUTHZ_CLIENT_ID"),
		authzClientSecret: os.Getenv("AUTHZ_CLIENT_SECRET"),
	})

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(grpcServer, shim)
	reflection.Register(grpcServer)

	log.Printf("[ExtSvc] ext_proc listening on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
