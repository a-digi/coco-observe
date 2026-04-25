// Package pusher sends metric batches to the aggregator over HTTPS.
// Auth uses HMAC-SHA256: the request body is signed with the API secret
// and the signature is sent in X-Observe-Signature alongside the API key
// in X-Observe-Key.
package pusher

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a-digi/coco-observe/payload"
)

// Pusher sends batches to the aggregator.
type Pusher struct {
	url       string
	apiKey    string
	apiSecret string
	client    *http.Client
}

// New constructs a Pusher. url must be the full push endpoint
// (e.g. https://iam.example.com/api/v1/admin/observe/push).
func New(url, apiKey, apiSecret string) *Pusher {
	return &Pusher{
		url:       url,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Push serialises the batch and sends it to the aggregator.
// Returns a non-nil error on any HTTP or transport failure.
func (p *Pusher) Push(batch *payload.Batch) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("push: marshal: %w", err)
	}

	sig := sign(body, p.apiSecret)

	req, err := http.NewRequest(http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Observe-Key", p.apiKey)
	req.Header.Set("X-Observe-Signature", sig)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("push: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push: aggregator returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// sign returns hex(HMAC-SHA256(body, secret)).
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
