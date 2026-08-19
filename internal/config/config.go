package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const configDir = ".config/plaud"
const configFile = "token.json"

// Config holds the persisted authentication state.
type Config struct {
	AccessToken string `json:"access_token"`
	BaseURL     string `json:"base_url"`
	DeviceID    string `json:"device_id"`
	WhisperURL  string `json:"whisper_url,omitempty"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// Load reads the config from disk, then lets the environment override it.
// Returns a zero Config (not an error) if the file doesn't exist, so an
// environment holding PLAUD_TOKEN needs no config file at all.
func Load() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}

	cfg := Config{}
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg.applyEnv()
	return &cfg, nil
}

// applyEnv lets environment variables stand in for the config file, which is
// what makes the CLI usable in a container, a CI job or someone else's machine
// where no interactive login has ever run.
func (c *Config) applyEnv() {
	if v := os.Getenv("PLAUD_TOKEN"); v != "" {
		c.AccessToken = v
	}
	if v := os.Getenv("PLAUD_DEVICE_ID"); v != "" {
		c.DeviceID = v
	}
	if c.DeviceID == "" && c.AccessToken != "" {
		// Nothing on disk to hold a random device ID, and a new one on every
		// invocation looks like a new device to the API. Derive a stable one
		// from the token instead.
		sum := sha256.Sum256([]byte(c.AccessToken))
		c.DeviceID = hex.EncodeToString(sum[:8])
	}
}

// Save writes the config to disk with restricted permissions.
func (c *Config) Save() error {
	p, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// GenerateDeviceID creates a random 16-character hex string.
func GenerateDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// EnsureDeviceID returns the existing device ID or generates a new one.
func (c *Config) EnsureDeviceID() string {
	if c.DeviceID == "" {
		c.DeviceID = GenerateDeviceID()
	}
	return c.DeviceID
}

// BaseURLOrDefault returns the configured base URL or the default.
func (c *Config) BaseURLOrDefault() string {
	if env := os.Getenv("PLAUD_API_URL"); env != "" {
		return env
	}
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.plaud.ai"
}
