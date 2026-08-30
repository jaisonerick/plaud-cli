package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jaisonerick/plaud-cli/internal/api"
)

func TestScheme(t *testing.T) {
	session := &api.Session{UserToken: "ut", ExpiresAt: 1}

	for _, c := range []struct {
		name       string
		cfg        Config
		want       Scheme
		superseded bool
	}{
		{"nothing stored", Config{}, NoCredential, false},
		{"a bearer token in the file", Config{AccessToken: "jwt"}, BearerScheme, true},
		{"a v3 session", Config{Session: session}, SessionScheme, false},
		// Only a file an older client wrote holds both, and the session is the
		// half that still works.
		{"a session beside the token it replaced", Config{AccessToken: "jwt", Session: session}, SessionScheme, false},
		// A session the server never established is not one to report.
		{"an empty session", Config{Session: &api.Session{}}, NoCredential, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.Scheme(); got != c.want {
				t.Errorf("Scheme() = %v, want %v", got, c.want)
			}
			if got := c.cfg.Superseded(); got != c.superseded {
				t.Errorf("Superseded() = %v, want %v", got, c.superseded)
			}
		})
	}
}

// A token handed to a container in PLAUD_TOKEN is the old scheme and is still
// not worth reporting: there is no login to run there, and a session
// established would have nowhere to live.
func TestTokenFromEnvIsNotSuperseded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PLAUD_TOKEN", "jwt-from-the-environment")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if got := cfg.Scheme(); got != BearerScheme {
		t.Errorf("Scheme() = %v, want %v", got, BearerScheme)
	}
	if cfg.Superseded() {
		t.Error("Superseded() = true for a token that came from PLAUD_TOKEN")
	}
}

// The same token in the file is one somebody signed in with, and is the case
// this whole thing exists to catch.
func TestTokenFromFileIsSuperseded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PLAUD_TOKEN", "")

	dir := filepath.Join(home, ".config", "plaud")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"access_token":"jwt"}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !cfg.Superseded() {
		t.Error("Superseded() = false for a bearer token in token.json")
	}
}
