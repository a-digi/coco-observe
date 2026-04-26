// Package store manages the hot SQLite database and archives.
// When metric_batches exceeds maxEntries the live data is vacuumed into
// a timestamped archive file and the hot table is cleared.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	maxEntries = 10_000_000
	hotDBName  = "observe-hot.db"
)

// Store wraps the hot SQLite database.
type Store struct {
	db      *sql.DB
	dataDir string
}

// Agent is a registered agent record.
type Agent struct {
	ID           string
	Name         string
	APIKey       string
	APISecretRaw string // only populated on CreateAgent; never persisted in plain form
	RegisteredAt time.Time
	LastSeenAt   *time.Time
	Enabled      bool
	BinaryAmd64  string // path to stored per-agent linux/amd64 binary, empty until generated
	BinaryArm64  string // path to stored per-agent linux/arm64 binary, empty until generated
	ProcessesJSON string // JSON array of {name} targets
	TrackOS      bool   // whether the agent collects host OS metrics
}

// Batch is a stored metric batch row returned by queries.
type Batch struct {
	ID         int64           `json:"id"`
	AgentID    string          `json:"agent_id"`
	CapturedAt time.Time       `json:"captured_at"`
	ReceivedAt time.Time       `json:"received_at"`
	Sequence   int64           `json:"sequence"`
	Buffered   bool            `json:"buffered"`
	Payload    json.RawMessage `json:"payload"`
}

// Open opens (or creates) the hot database at dataDir/observe-hot.db.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("store: mkdir: %w", err)
	}
	path := filepath.Join(dataDir, hotDBName)
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db, dataDir: dataDir}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// --- agents ---

// CreateAgent generates credentials, inserts the agent record, and returns
// it with APISecretRaw populated (the only time the plaintext secret is available).
func (s *Store) CreateAgent(name, processesJSON string, trackOS bool) (*Agent, error) {
	key, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if processesJSON == "" || processesJSON == "null" {
		processesJSON = "[]"
	}

	_, err = s.db.Exec(
		`INSERT INTO agents (id, name, api_key, api_secret, registered_at, enabled, processes, track_os)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		key, name, key, secret, now.Format(time.RFC3339), processesJSON, boolToInt(trackOS),
	)
	if err != nil {
		return nil, fmt.Errorf("store: create agent: %w", err)
	}
	return &Agent{
		ID:            key,
		Name:          name,
		APIKey:        key,
		APISecretRaw:  secret,
		RegisteredAt:  now,
		Enabled:       true,
		ProcessesJSON: processesJSON,
		TrackOS:       trackOS,
	}, nil
}

// AgentByID returns the agent and its raw API secret by agent ID, or sql.ErrNoRows.
func (s *Store) AgentByID(id string) (*Agent, string, error) {
	row := s.db.QueryRow(
		`SELECT id, name, api_key, api_secret, registered_at, last_seen_at, enabled,
		        COALESCE(binary_amd64,''), COALESCE(binary_arm64,''),
		        COALESCE(processes,'[]'), COALESCE(track_os,1)
		 FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

// AgentByKey returns the agent and its raw API secret, or sql.ErrNoRows.
func (s *Store) AgentByKey(apiKey string) (*Agent, string, error) {
	row := s.db.QueryRow(
		`SELECT id, name, api_key, api_secret, registered_at, last_seen_at, enabled,
		        COALESCE(binary_amd64,''), COALESCE(binary_arm64,''),
		        COALESCE(processes,'[]'), COALESCE(track_os,1)
		 FROM agents WHERE api_key = ?`, apiKey)
	return scanAgent(row)
}

// SetAgentBinaries stores the paths of the pre-generated per-agent binaries.
func (s *Store) SetAgentBinaries(id, amd64Path, arm64Path string) error {
	_, err := s.db.Exec(
		`UPDATE agents SET binary_amd64 = ?, binary_arm64 = ? WHERE id = ?`,
		amd64Path, arm64Path, id)
	return err
}

func scanAgent(row interface{ Scan(...any) error }) (*Agent, string, error) {
	var (
		a           Agent
		rawSecret   string
		registeredS string
		lastSeenS   *string
		enabledInt  int
		trackOSInt  int
	)
	if err := row.Scan(&a.ID, &a.Name, &a.APIKey, &rawSecret,
		&registeredS, &lastSeenS, &enabledInt,
		&a.BinaryAmd64, &a.BinaryArm64,
		&a.ProcessesJSON, &trackOSInt); err != nil {
		return nil, "", err
	}
	a.RegisteredAt, _ = time.Parse(time.RFC3339, registeredS)
	a.Enabled = enabledInt == 1
	a.TrackOS = trackOSInt == 1
	if lastSeenS != nil {
		t, _ := time.Parse(time.RFC3339, *lastSeenS)
		a.LastSeenAt = &t
	}
	return &a, rawSecret, nil
}

// ListAgents returns all agents ordered by registered_at.
func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(
		`SELECT id, name, api_key, registered_at, last_seen_at, enabled,
		        COALESCE(binary_amd64,''), COALESCE(binary_arm64,''),
		        COALESCE(processes,'[]'), COALESCE(track_os,1)
		 FROM agents ORDER BY registered_at`)
	if err != nil {
		return nil, fmt.Errorf("store: list agents: %w", err)
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var a Agent
		var registeredS string
		var lastSeenS *string
		var enabledInt, trackOSInt int
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKey,
			&registeredS, &lastSeenS, &enabledInt,
			&a.BinaryAmd64, &a.BinaryArm64,
			&a.ProcessesJSON, &trackOSInt); err != nil {
			return nil, err
		}
		a.RegisteredAt, _ = time.Parse(time.RFC3339, registeredS)
		a.Enabled = enabledInt == 1
		a.TrackOS = trackOSInt == 1
		if lastSeenS != nil {
			t, _ := time.Parse(time.RFC3339, *lastSeenS)
			a.LastSeenAt = &t
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// UpdateAgent updates the process list and track_os flag for an existing agent.
func (s *Store) UpdateAgent(id, processesJSON string, trackOS bool) error {
	if processesJSON == "" || processesJSON == "null" {
		processesJSON = "[]"
	}
	_, err := s.db.Exec(
		`UPDATE agents SET processes = ?, track_os = ? WHERE id = ?`,
		processesJSON, boolToInt(trackOS), id)
	return err
}

// DeleteAgent removes an agent and cascades to its batches.
func (s *Store) DeleteAgent(id string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

// TouchAgent updates last_seen_at to now.
func (s *Store) TouchAgent(id string) {
	_, _ = s.db.Exec(
		`UPDATE agents SET last_seen_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
}

// --- batches ---

// HasRecentBatch reports whether any batch from agentID was received within
// the last window duration. Used to deduplicate replay bursts.
func (s *Store) HasRecentBatch(agentID string, window time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)
	var exists int
	err := s.db.QueryRow(
		`SELECT EXISTS(
		     SELECT 1 FROM metric_batches
		     WHERE agent_id = ? AND received_at > ?
		 )`, agentID, cutoff,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: has recent batch: %w", err)
	}
	return exists == 1, nil
}

// InsertBatch stores a received metric batch and triggers archival if needed.
func (s *Store) InsertBatch(agentID string, capturedAt time.Time, sequence int64, buffered bool, payloadJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO metric_batches (agent_id, captured_at, received_at, sequence, buffered, payload)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		agentID,
		capturedAt.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		sequence,
		boolToInt(buffered),
		payloadJSON,
	)
	if err != nil {
		return fmt.Errorf("store: insert batch: %w", err)
	}
	go s.archiveIfNeeded()
	return nil
}

// BatchPage is a paginated result for raw batch queries.
type BatchPage struct {
	Total int     `json:"total"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
	Items []Batch `json:"items"`
}

// QueryBatchesPaginated returns one page of batches for agentID in [from, to], newest first.
func (s *Store) QueryBatchesPaginated(agentID string, from, to time.Time, page, limit int) (BatchPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if page < 0 {
		page = 0
	}
	fromS := from.UTC().Format(time.RFC3339)
	toS := to.UTC().Format(time.RFC3339)

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM metric_batches WHERE agent_id = ? AND captured_at BETWEEN ? AND ?`,
		agentID, fromS, toS,
	).Scan(&total); err != nil {
		return BatchPage{}, fmt.Errorf("store: count page: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT id, agent_id, captured_at, received_at, sequence, buffered, payload
		 FROM metric_batches
		 WHERE agent_id = ? AND captured_at BETWEEN ? AND ?
		 ORDER BY captured_at DESC LIMIT ? OFFSET ?`,
		agentID, fromS, toS, limit, page*limit,
	)
	if err != nil {
		return BatchPage{}, fmt.Errorf("store: query page: %w", err)
	}
	defer rows.Close()
	items, err := scanBatches(rows)
	if err != nil {
		return BatchPage{}, err
	}
	return BatchPage{Total: total, Page: page, Limit: limit, Items: items}, nil
}

// QueryBatches returns batches for agentID in [from, to], newest first.
func (s *Store) QueryBatches(agentID string, from, to time.Time, limit int) ([]Batch, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(
		`SELECT id, agent_id, captured_at, received_at, sequence, buffered, payload
		 FROM metric_batches
		 WHERE agent_id = ? AND captured_at BETWEEN ? AND ?
		 ORDER BY captured_at DESC LIMIT ?`,
		agentID,
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()
	return scanBatches(rows)
}

// LatestBatch returns the most-recent batch for agentID, or sql.ErrNoRows.
func (s *Store) LatestBatch(agentID string) (*Batch, error) {
	row := s.db.QueryRow(
		`SELECT id, agent_id, captured_at, received_at, sequence, buffered, payload
		 FROM metric_batches WHERE agent_id = ?
		 ORDER BY captured_at DESC LIMIT 1`, agentID)
	return scanOneBatch(row)
}

// --- archive ---

func (s *Store) archiveIfNeeded() {
	var count int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM metric_batches`).Scan(&count); err != nil {
		return
	}
	if count < maxEntries {
		return
	}
	archivePath := filepath.Join(s.dataDir,
		fmt.Sprintf("observe-archive-%d.db", time.Now().UnixNano()))
	if _, err := s.db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, archivePath)); err != nil {
		fmt.Printf("store: archive vacuum: %v\n", err)
		return
	}
	if _, err := s.db.Exec(`DELETE FROM metric_batches`); err != nil {
		fmt.Printf("store: archive clear: %v\n", err)
	}
}

// QueryArchive opens an archive file read-only and queries its batches.
func QueryArchive(archivePath, agentID string, from, to time.Time, limit int) ([]Batch, error) {
	db, err := sql.Open("sqlite3", "file:"+archivePath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("store: open archive: %w", err)
	}
	defer db.Close()

	if limit <= 0 {
		limit = 500
	}
	rows, err := db.Query(
		`SELECT id, agent_id, captured_at, received_at, sequence, buffered, payload
		 FROM metric_batches
		 WHERE agent_id = ? AND captured_at BETWEEN ? AND ?
		 ORDER BY captured_at DESC LIMIT ?`,
		agentID,
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: archive query: %w", err)
	}
	defer rows.Close()
	return scanBatches(rows)
}

// --- helpers ---

func scanBatches(rows *sql.Rows) ([]Batch, error) {
	batches := make([]Batch, 0)
	for rows.Next() {
		b, err := scanOneBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, *b)
	}
	return batches, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOneBatch(s scanner) (*Batch, error) {
	var b Batch
	var capturedS, receivedS, payloadStr string
	var bufferedInt int
	if err := s.Scan(&b.ID, &b.AgentID, &capturedS, &receivedS,
		&b.Sequence, &bufferedInt, &payloadStr); err != nil {
		return nil, err
	}
	b.CapturedAt, _ = time.Parse(time.RFC3339, capturedS)
	b.ReceivedAt, _ = time.Parse(time.RFC3339, receivedS)
	b.Buffered = bufferedInt == 1
	b.Payload = json.RawMessage(payloadStr)
	return &b, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
