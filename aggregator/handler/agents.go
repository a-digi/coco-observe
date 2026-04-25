package handler

import (
	"encoding/json"
	"net/http"
	"path"

	"github.com/a-digi/coco-observe/aggregator/store"
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
	Store    *store.Store
	BasePath string
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
	case r.Method == http.MethodDelete && !isCollection:
		h.delete(w, tail)
	default:
		jsonError(w, "not_found", http.StatusNotFound)
	}
}

func (h *AgentsHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	agent, err := h.Store.CreateAgent(body.Name)
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         agent.ID,
		"name":       agent.Name,
		"api_key":    agent.APIKey,
		"api_secret": agent.APISecretRaw, // shown exactly once
	})
}

func (h *AgentsHandler) list(w http.ResponseWriter, r *http.Request) {
	agents, err := h.Store.ListAgents()
	if err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	// Never expose the api_secret in list responses.
	type agentView struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		APIKey       string  `json:"api_key"`
		RegisteredAt string  `json:"registered_at"`
		LastSeenAt   *string `json:"last_seen_at"`
		Enabled      bool    `json:"enabled"`
	}
	var views []agentView
	for _, a := range agents {
		v := agentView{
			ID:           a.ID,
			Name:         a.Name,
			APIKey:       a.APIKey,
			RegisteredAt: a.RegisteredAt.Format("2006-01-02T15:04:05Z"),
			Enabled:      a.Enabled,
		}
		if a.LastSeenAt != nil {
			s := a.LastSeenAt.Format("2006-01-02T15:04:05Z")
			v.LastSeenAt = &s
		}
		views = append(views, v)
	}
	writeJSON(w, views)
}

func (h *AgentsHandler) delete(w http.ResponseWriter, id string) {
	if err := h.Store.DeleteAgent(id); err != nil {
		jsonError(w, "store error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
