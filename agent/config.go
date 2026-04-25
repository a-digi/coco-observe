package agent

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigMagic is the delimiter appended to the binary by the download handler.
// The agent searches backwards through its own executable to find it.
const ConfigMagic = "\n---OBSERVE-CONFIG-V1---\n"

// Config is the agent's full runtime configuration.
type Config struct {
	APIKey          string          `yaml:"api_key"`
	APISecret       string          `yaml:"api_secret"`
	AggregatorURL   string          `yaml:"aggregator_url"`
	PushInterval    time.Duration   `yaml:"push_interval"`
	BufferDir       string          `yaml:"buffer_dir"`
	BufferRetention time.Duration   `yaml:"buffer_retention"`
	TrackOS         bool            `yaml:"track_os"`
	Processes       []ProcessTarget `yaml:"processes"`
}

// ProcessTarget describes one process to monitor by name via /proc.
type ProcessTarget struct {
	Name string `yaml:"name"`
}

// LoadEmbeddedConfig reads the config appended to this binary by the
// download handler. Returns an error when no embedded config is found,
// which the caller uses as a signal to fall back to a config file.
func LoadEmbeddedConfig() (*Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("embedded config: get executable path: %w", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return nil, fmt.Errorf("embedded config: read executable: %w", err)
	}
	magic := []byte(ConfigMagic)
	idx := bytes.LastIndex(data, magic)
	if idx < 0 {
		return nil, fmt.Errorf("embedded config: magic header not found")
	}
	cfgBytes := data[idx+len(magic):]
	var cfg Config
	if err := yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, fmt.Errorf("embedded config: parse: %w", err)
	}
	applyEnvOverrides(&cfg)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfig reads the YAML file at path and applies env var overrides.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyEnvOverrides(&cfg)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("OBSERVE_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("OBSERVE_API_SECRET"); v != "" {
		cfg.APISecret = v
	}
	if v := os.Getenv("OBSERVE_AGGREGATOR_URL"); v != "" {
		cfg.AggregatorURL = v
	}
	if v := os.Getenv("OBSERVE_BUFFER_DIR"); v != "" {
		cfg.BufferDir = v
	}
}

func (c *Config) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("agent config: api_key is required")
	}
	if c.APISecret == "" {
		return fmt.Errorf("agent config: api_secret is required")
	}
	if c.AggregatorURL == "" {
		return fmt.Errorf("agent config: aggregator_url is required")
	}
	if c.PushInterval <= 0 {
		c.PushInterval = 180 * time.Second
	}
	if c.BufferRetention <= 0 {
		c.BufferRetention = 24 * time.Hour
	}
	if c.BufferDir == "" {
		c.BufferDir = "/var/lib/observe-agent/buffer"
	}
	return nil
}
