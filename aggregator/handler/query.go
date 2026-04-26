package handler

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-digi/coco-observe/aggregator/store"
)

// QueryHandler serves metric batch queries for the admin UI.
// GET /observe/metrics?agent_id=<id>&from=<rfc3339>&to=<rfc3339>&limit=<n>
// GET /observe/metrics?agent_id=<id>&range=1h|6h|24h|7d  (shorthand)
// GET /observe/metrics/latest?agent_id=<id>
// GET /observe/metrics/archive?agent_id=<id>&file=<name>&from=...&to=...
type QueryHandler struct {
	Store   *store.Store
	DataDir string
}

func (h *QueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	agentID := q.Get("agent_id")
	if agentID == "" {
		jsonError(w, "agent_id required", http.StatusBadRequest)
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/latest") || q.Get("latest") == "1":
		h.serveLatest(w, agentID)
	case strings.HasSuffix(r.URL.Path, "/raw"):
		h.serveRaw(w, agentID, q.Get("range"), q.Get("from"), q.Get("to"), q.Get("page"), q.Get("limit"))
	case q.Get("file") != "":
		h.serveArchive(w, agentID, q.Get("file"), q.Get("from"), q.Get("to"))
	default:
		h.serveRange(w, agentID, q.Get("from"), q.Get("to"), q.Get("range"))
	}
}

func (h *QueryHandler) serveLatest(w http.ResponseWriter, agentID string) {
	batch, err := h.Store.LatestBatch(agentID)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, batch)
}

func (h *QueryHandler) serveRange(w http.ResponseWriter, agentID, fromStr, toStr, rangeStr string) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if rangeStr != "" {
		d, ok := parseRange(rangeStr)
		if !ok {
			jsonError(w, "invalid range, use 1h|6h|24h|7d", http.StatusBadRequest)
			return
		}
		from = to.Add(-d)
	} else {
		if fromStr != "" {
			if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
				from = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse(time.RFC3339, toStr); err == nil {
				to = t
			}
		}
	}

	batches, err := h.Store.QueryBatches(agentID, from, to, 0)
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, batches)
}

func (h *QueryHandler) serveRaw(w http.ResponseWriter, agentID, rangeStr, fromStr, toStr, pageStr, limitStr string) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if rangeStr != "" {
		if d, ok := parseRange(rangeStr); ok {
			from = to.Add(-d)
		} else {
			jsonError(w, "invalid range, use 1h|6h|24h|7d", http.StatusBadRequest)
			return
		}
	} else {
		if fromStr != "" {
			if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
				from = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse(time.RFC3339, toStr); err == nil {
				to = t
			}
		}
	}

	page := 0
	if n, err := strconv.Atoi(pageStr); err == nil && n >= 0 {
		page = n
	}
	limit := 50
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
		limit = n
	}

	result, err := h.Store.QueryBatchesPaginated(agentID, from, to, page, limit)
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (h *QueryHandler) serveArchive(w http.ResponseWriter, agentID, file, fromStr, toStr string) {
	// Only allow plain filenames, no path traversal.
	if filepath.Base(file) != file {
		jsonError(w, "invalid file", http.StatusBadRequest)
		return
	}
	archivePath := filepath.Join(h.DataDir, file)

	to := time.Now().UTC()
	from := to.Add(-7 * 24 * time.Hour)
	if fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	batches, err := store.QueryArchive(archivePath, agentID, from, to, 0)
	if err != nil {
		jsonError(w, "archive error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, batches)
}

func parseRange(s string) (time.Duration, bool) {
	switch s {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	}
	return 0, false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
