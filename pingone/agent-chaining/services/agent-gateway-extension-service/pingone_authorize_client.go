package main

import (
	"log"
)

type pingoneAuthorizeClient struct{}

// Decide is intentionally permit-all during plumbing mode. Replace this
// implementation with the PingOne Authorize client after the chain is proven.
func (c *pingoneAuthorizeClient) Decide(userSub, agentClientID, action string, amountCents, requestHour int) (bool, error) {
	log.Printf("[ExtSvc] PERMIT (permit-all mode) subject=%s actor=%s action=%s", userSub, agentClientID, action)
	return true, nil
}
