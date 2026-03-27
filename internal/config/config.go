package config

import (
	"crypto/rand"
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
	AccessToken    string `json:"access_token"`
	BaseURL        string `json:"base_url"`
	DeviceID       string `json:"device_id"`
	ModalTokenID   string `json:"modal_token_id,omitempty"`
	ModalTokenSecret string `json:"modal_token_secret,omitempty"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// Load reads the config from disk. Returns a zero Config (not an error) if the file doesn't exist.
func Load() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
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
