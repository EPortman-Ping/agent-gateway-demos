package main

import (
	"log"
	"sync"
)

// pingoneAuthorizeClient calls PingOne Authorize for a compound PERMIT/DENY
// decision using both user and agent attributes.
type pingoneAuthorizeClient struct {
	decisionEndpoint string
	tokenEndpoint    string
	clientID         string
	clientSecret     string

	mu    sync.Mutex
	token cachedToken
}

// Decide sends compound attributes to PingOne Authorize and returns true for
// PERMIT. The policy can check any combination of user sub, agent client_id,
// tool name, payment amount, and request hour.
//
// TODO: configure PingOne Authorize policy before enabling this check.
func (c *pingoneAuthorizeClient) Decide(userSub, agentClientID, toolName string, amountCents, requestHour int) (bool, error) {
	// TODO: wire up the real HTTP call once the PingOne Authorize policy is configured.
	log.Printf("[ExtSvc] PingOne Authorize skipped (policy not yet configured) — PERMIT user=%s agent=%s tool=%s", userSub, agentClientID, toolName)
	return true, nil
}
