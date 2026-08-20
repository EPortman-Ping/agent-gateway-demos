package main

import (
	"log"

	"github.com/mark3labs/mcp-go/server"
)

func newMCPServer(usersClient *pingoneUsersClient) *server.StreamableHTTPServer {
	s := server.NewMCPServer("hr-directory-server", "1.0.0",
		server.WithToolCapabilities(false),
	)
	registerHRTools(s, usersClient)
	return server.NewStreamableHTTPServer(s, server.WithStateLess(true))
}

func newPingOneUsersClient() *pingoneUsersClient {
	mgmtClientID := requireEnv("PINGONE_MGMT_CLIENT_ID")
	mgmtClientSecret := requireEnv("PINGONE_MGMT_CLIENT_SECRET")

	envID, apiBase, err := parsePingOneCoords(idpIssuer)
	if err != nil {
		log.Fatalf("cannot derive PingOne coordinates from IDP_ISSUER %q: %v", idpIssuer, err)
	}

	return &pingoneUsersClient{
		envID:        envID,
		apiBase:      apiBase,
		tokenURL:     idpIssuer + "/token",
		clientID:     mgmtClientID,
		clientSecret: mgmtClientSecret,
	}
}
