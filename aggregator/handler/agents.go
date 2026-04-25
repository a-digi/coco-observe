package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/a-digi/coco-observe/agent"
	"github.com/a-digi/coco-observe/aggregator/store"
	"gopkg.in/yaml.v3"
)

// AgentsHandler manages agent registration records.
//
//	POST   /…/observe/agents        — create agent, returns credentials (secret shown once)
//	GET    /…/observe/agents        — list all agents
//	DELETE /…/observe/agents/<id>   — delete agent and its batches
//
// BasePath is the mount prefix the host routes to this handler (e.g.
// "/api/v1/admin/observe/agents"). It is stripped before dispatching so
// the handler works regardless of where it is mounted.
type AgentsHandler struct {
	Store         *store.Store
	BasePath      string
	DataDir       string
	AggregatorURL string
	// EmbeddedAmd64 / EmbeddedArm64 are the base Linux agent binaries embedded
	// into the server binary at build time. When non-empty, per-agent binaries
	// are generated (base + embedded config) on agent creation and stored under
	// DataDir/agents/<id>/.
	EmbeddedAmd64 []byte
	EmbeddedArm64 []byte
}

func (h *AgentsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the known mount prefix; what remains is "" or "/<id>".
	tail := path.Base(r.URL.Path)
	isCollection := tail == path.Base(h.BasePath)

	switch {
	case r.Method == http.MethodPost && isCollection:
		h.create(w, r)
	case r.Method == http.MethodGet && isCollection:
		h.list(w, r)
	case r.Method == http.MethodPatch && !isCollection:
		h.update(w, r, tail)
	case r.Method == http.MethodDelete && !isCollection:
		h.delete(w, tail)
	default:
		jsonError(w, "not_found", http.StatusNotFound)
	}
}

type processTarget struct {
	Name string `json:"name" yaml:"name"`
}

func (h *AgentsHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string          `json:"name"`
		Processes []processTarget `json:"processes"`
		TrackOS   *bool           `json:"track_os"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	trackOS := true // default: track OS
	if body.TrackOS != nil {
		trackOS = *body.TrackOS
	}

	if body.Processes == nil {
		body.Processes = []processTarget{}
	}
	processesJSON, err := json.Marshal(body.Processes)
	if err != nil {
		jsonError(w, "invalid processes", http.StatusBadRequest)
		return
	}

	a, err := h.Store.CreateAgent(body.Name, string(processesJSON), trackOS)
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}

	// Generate and store per-agent binaries for both archs from the embedded bases.
	amd64Path, arm64Path := h.generateBinaries(a, a.APISecretRaw)
	if amd64Path != "" || arm64Path != "" {
		_ = h.Store.SetAgentBinaries(a.ID, amd64Path, arm64Path)
	}

	var procs []processTarget
	_ = json.Unmarshal([]byte(a.ProcessesJSON), &procs)
	if procs == nil {
		procs = []processTarget{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         a.ID,
		"name":       a.Name,
		"api_key":    a.APIKey,
		"api_secret": a.APISecretRaw, // shown exactly once
		"processes":  procs,
		"track_os":   a.TrackOS,
	})
}

// generateBinaries appends the agent-specific config to each embedded base binary
// and writes the result to DataDir/agents/<id>/. Returns the paths that were written.
func (h *AgentsHandler) generateBinaries(a *store.Agent, rawSecret string) (amd64Path, arm64Path string) {
	cfgYAML := generateEmbeddedConfig(a, rawSecret, h.AggregatorURL)
	dir := filepath.Join(h.DataDir, "agents", a.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	write := func(base []byte, arch string) string {
		if len(base) == 0 {
			return ""
		}
		out := make([]byte, 0, len(base)+len(agent.ConfigMagic)+len(cfgYAML))
		out = append(out, base...)
		out = append(out, []byte(agent.ConfigMagic)...)
		out = append(out, cfgYAML...)
		p := filepath.Join(dir, fmt.Sprintf("observe-agent-linux-%s", arch))
		if err := os.WriteFile(p, out, 0755); err != nil {
			return ""
		}
		return p
	}

	amd64Path = write(h.EmbeddedAmd64, "amd64")
	arm64Path = write(h.EmbeddedArm64, "arm64")
	return
}

func (h *AgentsHandler) list(w http.ResponseWriter, r *http.Request) {
	agents, err := h.Store.ListAgents()
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	// Never expose the api_secret in list responses.
	type agentView struct {
		ID           string            `json:"id"`
		Name         string            `json:"name"`
		APIKey       string            `json:"api_key"`
		RegisteredAt string            `json:"registered_at"`
		LastSeenAt   *string           `json:"last_seen_at"`
		Enabled      bool              `json:"enabled"`
		HasBinary    bool              `json:"has_binary"`
		TrackOS      bool              `json:"track_os"`
		Processes    []processTarget   `json:"processes"`
	}
	views := make([]agentView, 0, len(agents))
	for _, a := range agents {
		var procs []processTarget
		_ = json.Unmarshal([]byte(a.ProcessesJSON), &procs)
		if procs == nil {
			procs = []processTarget{}
		}
		v := agentView{
			ID:           a.ID,
			Name:         a.Name,
			APIKey:       a.APIKey,
			RegisteredAt: a.RegisteredAt.Format("2006-01-02T15:04:05Z"),
			Enabled:      a.Enabled,
			HasBinary:    a.BinaryAmd64 != "" || a.BinaryArm64 != "",
			TrackOS:      a.TrackOS,
			Processes:    procs,
		}
		if a.LastSeenAt != nil {
			s := a.LastSeenAt.Format("2006-01-02T15:04:05Z")
			v.LastSeenAt = &s
		}
		views = append(views, v)
	}
	writeJSON(w, views)
}

type embeddedConfigYAML struct {
	APIKey          string          `yaml:"api_key"`
	APISecret       string          `yaml:"api_secret"`
	AggregatorURL   string          `yaml:"aggregator_url"`
	PushInterval    string          `yaml:"push_interval"`
	BufferDir       string          `yaml:"buffer_dir"`
	BufferRetention string          `yaml:"buffer_retention"`
	TrackOS         bool            `yaml:"track_os"`
	Processes       []processTarget `yaml:"processes"` // each entry has only "name"
}

func generateEmbeddedConfig(a *store.Agent, rawSecret, aggregatorURL string) []byte {
	var procs []processTarget
	_ = json.Unmarshal([]byte(a.ProcessesJSON), &procs)
	if procs == nil {
		procs = []processTarget{}
	}

	cfg := embeddedConfigYAML{
		APIKey:          a.APIKey,
		APISecret:       rawSecret,
		AggregatorURL:   aggregatorURL,
		PushInterval:    "180s",
		BufferDir:       "/var/lib/observe-agent/buffer",
		BufferRetention: "24h",
		TrackOS:         a.TrackOS,
		Processes:       procs,
	}
	out, _ := yaml.Marshal(cfg)
	return out
}


func (h *AgentsHandler) update(w http.ResponseWriter, r *http.Request, id string) {
	a, rawSecret, err := h.Store.AgentByID(id)
	if err != nil {
		jsonError(w, "not found", http.StatusNotFound)
		return
	}

	var body struct {
		Processes []processTarget `json:"processes"`
		TrackOS   *bool           `json:"track_os"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}

	if body.Processes == nil {
		body.Processes = []processTarget{}
	}
	processesJSON, err := json.Marshal(body.Processes)
	if err != nil {
		jsonError(w, "invalid processes", http.StatusBadRequest)
		return
	}

	if body.TrackOS != nil {
		a.TrackOS = *body.TrackOS
	}
	a.ProcessesJSON = string(processesJSON)

	if err := h.Store.UpdateAgent(id, a.ProcessesJSON, a.TrackOS); err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}

	// Regenerate binaries with updated config so user can download fresh binary.
	amd64Path, arm64Path := h.generateBinaries(a, rawSecret)
	if amd64Path != "" || arm64Path != "" {
		_ = h.Store.SetAgentBinaries(id, amd64Path, arm64Path)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AgentsHandler) delete(w http.ResponseWriter, id string) {
	// Remove stored binaries before deleting the DB record.
	dir := filepath.Join(h.DataDir, "agents", id)
	_ = os.RemoveAll(dir)

	if err := h.Store.DeleteAgent(id); err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
