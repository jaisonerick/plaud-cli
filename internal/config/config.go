package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaisonerick/plaud-cli/internal/api"
)

const configDir = ".config/plaud"
const configFile = "token.json"

// Config holds the persisted authentication state.
type Config struct {
	AccessToken string `json:"access_token"`
	BaseURL     string `json:"base_url"`
	DeviceID    string `json:"device_id"`
	WhisperURL  string `json:"whisper_url,omitempty"`

	// Session is what the v3 API hands over instead of a bearer token: a
	// cookie that authenticates and a second one that renews it. AccessToken
	// stays for PLAUD_TOKEN and `login --token`, and for an account the
	// migration has not reached.
	Session *api.Session `json:"session,omitempty"`

	// tokenFromEnv records that the bearer token arrived in PLAUD_TOKEN
	// rather than from the file. That is the difference between a token
	// handed to a container on purpose and a sign-in the old scheme left
	// behind, and only the second is worth telling anyone about.
	tokenFromEnv bool
}

// Authenticated reports whether there is anything here to make a call with.
func (c *Config) Authenticated() bool {
	return c.AccessToken != "" || c.Session.Valid()
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
		c.tokenFromEnv = true
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

// Scheme names what a stored credential authenticates with.
//
// The two are not interchangeable and their expiries mean opposite things: a
// v3 session lasts a day and buys itself another on every call, while a
// bearer token is a JWT valid for months that nothing here renews. Reporting
// one date without saying which scheme produced it tells nobody whether they
// have to act.
type Scheme int

const (
	// NoCredential is nothing to authenticate with.
	NoCredential Scheme = iota
	// SessionScheme is the v3 session: cookies, renewed as they are used.
	SessionScheme
	// BearerScheme is the token the API handed out before v3.
	BearerScheme
)

func (s Scheme) String() string {
	switch s {
	case SessionScheme:
		return "v3 session"
	case BearerScheme:
		return "bearer token, from before v3"
	default:
		return "none"
	}
}

// Scheme reports which credential a call would be made with.
//
// A session wins where both are present. `login` clears the bearer token it
// replaces, so the two only ever coexist in a file an older client wrote, and
// the session is the half that still works.
func (c *Config) Scheme() Scheme {
	switch {
	case c.Session.Valid():
		return SessionScheme
	case c.AccessToken != "":
		return BearerScheme
	default:
		return NoCredential
	}
}

// Superseded reports whether this sign-in is the pre-v3 one and is worth
// replacing with a session.
//
// A token that arrived in PLAUD_TOKEN is left alone however old the scheme
// behind it. It is how the CLI runs where no interactive login can happen, the
// environment is not this process's to rewrite, and a session established here
// would have nowhere to live: telling a container to go and log in is an
// instruction nobody in that container can carry out.
func (c *Config) Superseded() bool {
	return c.Scheme() == BearerScheme && !c.tokenFromEnv
}
