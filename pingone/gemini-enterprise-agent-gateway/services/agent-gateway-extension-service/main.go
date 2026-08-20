// Agent Gateway extension service — an Envoy ext_proc gRPC handler.
//
// The Agent Gateway calls this service for every MCP request egressing from a
// Gemini Enterprise app. The inbound bearer token is a PingOne user token
// minted by the Gemini Enterprise auth manager's 3-legged OAuth flow.
//
// Current behaviour (approve-all phase):
//  1. Validates the inbound user token (iss, aud, scope, sig via JWKS).
//  2. Logs the MCP method and tool name on tools/call.
//  3. Approves all requests — PingOne Authorize is not yet wired in.
//
// The original bearer token passes through unchanged to the HR MCP server,
// which performs its own JWT validation.
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
		toolURL:     os.Getenv("TOOL_URL"),
		idpEndpoint: os.Getenv("IDP_TOKEN_ENDPOINT"),
		idpAudience: os.Getenv("IDP_REQUIRED_AUDIENCE"),
		idpScope:    os.Getenv("IDP_SCOPE"),
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
