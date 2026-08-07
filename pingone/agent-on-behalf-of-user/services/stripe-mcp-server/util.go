package main

import (
	"log"
	"os"
	"strings"
)

const ctxKeyCallerEmail = "caller-email"
const ctxKeyGrantedScope = "granted-scope"

// requireEnv returns the value of the given environment variable or fatally exits if unset.
func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return val
}

// tokenHasScope reports whether the space-separated scope string contains target.
func tokenHasScope(scopeStr, target string) bool {
	for _, s := range strings.Fields(scopeStr) {
		if s == target {
			return true
		}
	}
	return false
}

