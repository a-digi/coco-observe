// Package auth verifies agent push requests.
// The agent signs the request body with HMAC-SHA256 using its API secret.
// The aggregator looks up the stored secret hash by API key and verifies.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Verify returns true when the HMAC-SHA256 of body keyed with secret
// matches the hex signature sent by the agent.
func Verify(body []byte, secret, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}
