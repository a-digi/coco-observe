package handler

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/a-digi/coco-observe/aggregator/auth"
	"github.com/a-digi/coco-observe/aggregator/store"
	"github.com/a-digi/coco-observe/payload"
)

// ReceiveHandler accepts push batches from agents.
// Auth: agent sends X-Observe-Key + X-Observe-Signature (HMAC-SHA256 of body
// keyed with the API secret). The aggregator looks up the raw secret by key
// and verifies the signature — same pattern as GitHub webhooks.
type ReceiveHandler struct {
	Store *store.Store
}

func (h *ReceiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := r.Header.Get("X-Observe-Key")
	signature := r.Header.Get("X-Observe-Signature")
	if apiKey == "" || signature == "" {
		jsonError(w, "missing credentials", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		jsonError(w, "read body", http.StatusBadRequest)
		return
	}

	agent, rawSecret, err := h.Store.AgentByKey(apiKey)
	if err == sql.ErrNoRows {
		jsonError(w, "unknown agent", http.StatusUnauthorized)
		return
	}
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	if !agent.Enabled {
		jsonError(w, "agent disabled", http.StatusForbidden)
		return
	}
	if !auth.Verify(body, rawSecret, signature) {
		jsonError(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var batch payload.Batch
	if err := json.Unmarshal(body, &batch); err != nil {
		jsonError(w, "invalid payload", http.StatusBadRequest)
		return
	}

	capturedAt, err := time.Parse(time.RFC3339, batch.Timestamp)
	if err != nil {
		capturedAt = time.Now().UTC()
	}

	if err := h.Store.InsertBatch(
		agent.ID, capturedAt, batch.Sequence, batch.Buffered, string(body),
	); err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}

	h.Store.TouchAgent(agent.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
