package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/a-digi/coco-observe/aggregator/store"
)

// AgentDownloadHandler serves the pre-generated per-agent binary produced at
// agent-creation time.
//
//	GET /…/observe/agents/{id}/download?arch=amd64|arm64
type AgentDownloadHandler struct {
	Store *store.Store
}

func (h *AgentDownloadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent ID: second-to-last segment of the path.
	// Pattern: /…/observe/agents/{id}/download
	agentID := path.Base(path.Dir(r.URL.Path))
	if agentID == "" || agentID == "." {
		jsonError(w, "agent id missing from path", http.StatusBadRequest)
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = "amd64"
	}
	if arch != "amd64" && arch != "arm64" {
		jsonError(w, "invalid arch: use amd64 or arm64", http.StatusBadRequest)
		return
	}

	agentRecord, _, err := h.Store.AgentByID(agentID)
	if err == sql.ErrNoRows {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}

	var binaryPath string
	if arch == "amd64" {
		binaryPath = agentRecord.BinaryAmd64
	} else {
		binaryPath = agentRecord.BinaryArm64
	}

	if binaryPath == "" {
		jsonError(w,
			fmt.Sprintf("binary for arch %s not available — the server was not built with embedded agent binaries", arch),
			http.StatusServiceUnavailable,
		)
		return
	}

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonError(w, "binary file missing on server — try re-creating the agent", http.StatusServiceUnavailable)
			return
		}
		jsonError(w, "failed to read agent binary", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("observe-agent-%s-%s", arch, agentRecord.Name)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
