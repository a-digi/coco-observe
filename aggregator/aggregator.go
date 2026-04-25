// Package aggregator is the entry point for host applications.
// Construct an Aggregator, then mount its handlers at whichever paths the
// host chooses. The host owns routing, auth middleware, and logging.
//
// Example (coco-iam):
//
//	agg, err := aggregator.New(aggregator.Config{DataDir: "/app/observe-data"})
//	handlerMap["ObservePushHandler"]   = agg.PushHandler()
//	handlerMap["ObserveQueryHandler"]  = agg.QueryHandler()
//	handlerMap["ObserveAgentsHandler"] = agg.AgentsHandler()
package aggregator

import (
	"fmt"
	"net/http"

	"github.com/a-digi/coco-observe/aggregator/handler"
	"github.com/a-digi/coco-observe/aggregator/store"
)

// Config holds the aggregator's startup options.
type Config struct {
	// DataDir is where the hot SQLite database and archive files are stored.
	DataDir string
}

// Aggregator wires the store and handlers together.
type Aggregator struct {
	store *store.Store
	cfg   Config
}

// New opens (or creates) the hot database and returns a ready Aggregator.
func New(cfg Config) (*Aggregator, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("aggregator: DataDir is required")
	}
	s, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("aggregator: %w", err)
	}
	return &Aggregator{store: s, cfg: cfg}, nil
}

// Close releases the database connection. Call on server shutdown.
func (a *Aggregator) Close() error {
	return a.store.Close()
}

// PushHandler accepts metric batches from agents.
// Mount at POST /…/observe/push (public — authenticated by HMAC).
func (a *Aggregator) PushHandler() http.Handler {
	return &handler.ReceiveHandler{Store: a.store}
}

// QueryHandler serves metric queries to the admin UI.
// Mount at GET /…/observe/metrics (requires observe:view scope).
func (a *Aggregator) QueryHandler() http.Handler {
	return &handler.QueryHandler{Store: a.store, DataDir: a.cfg.DataDir}
}

// AgentsHandler manages agent records and credentials.
// Mount at basePath (requires observe:manage scope).
// basePath must be the full path prefix, e.g. "/api/v1/admin/observe/agents".
func (a *Aggregator) AgentsHandler(basePath string) http.Handler {
	return &handler.AgentsHandler{Store: a.store, BasePath: basePath}
}
