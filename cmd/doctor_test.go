package cmd

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/config"
	"github.com/spf13/cobra"
)

// bearerToken is a JWT carrying nothing but the expiry doctor reads off it.
func bearerToken(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return "header." + payload + ".signature"
}

// withConfig swaps in the config a case is about and puts the old one back,
// since the command package reads it from a variable it shares.
func withConfig(t *testing.T, c *config.Config) {
	t.Helper()
	previous := cfg
	cfg = c
	t.Cleanup(func() { cfg = previous })
}

func TestSignInLinesNameTheScheme(t *testing.T) {
	t.Run("a v3 session is a fact rather than a deadline", func(t *testing.T) {
		withConfig(t, &config.Config{Session: &api.Session{
			UserToken: "ut",
			ExpiresAt: time.Now().Add(20 * time.Hour).Unix(),
		}})

		lines := signInLines()
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
		}
		if !strings.Contains(lines[0], "v3 session") {
			t.Errorf("scheme line does not name the scheme: %q", lines[0])
		}
		if strings.Contains(lines[0], "superseded") {
			t.Errorf("a current session was reported as superseded: %q", lines[0])
		}
		if !strings.Contains(lines[1], "renewed as it is used") {
			t.Errorf("expiry line reads as a deadline: %q", lines[1])
		}
	})

	t.Run("a bearer token is named and marked", func(t *testing.T) {
		withConfig(t, &config.Config{AccessToken: bearerToken(time.Now().Add(120 * 24 * time.Hour))})

		lines := signInLines()
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
		}
		if !strings.Contains(lines[0], "before v3") {
			t.Errorf("scheme line does not say which scheme: %q", lines[0])
		}
		if !strings.Contains(lines[0], "superseded") {
			t.Errorf("an old token was not marked: %q", lines[0])
		}
		if !strings.Contains(lines[1], "expires") {
			t.Errorf("expiry line missing: %q", lines[1])
		}
		if strings.Contains(lines[1], "renewed") {
			t.Errorf("a token nothing renews was reported as renewed: %q", lines[1])
		}
	})

	t.Run("a token whose expiry cannot be read still names the scheme", func(t *testing.T) {
		withConfig(t, &config.Config{AccessToken: "not-a-jwt"})

		lines := signInLines()
		if len(lines) != 1 {
			t.Fatalf("got %d lines, want 1: %q", len(lines), lines)
		}
		if !strings.Contains(lines[0], "before v3") {
			t.Errorf("scheme line does not say which scheme: %q", lines[0])
		}
	})

	t.Run("nothing signed in", func(t *testing.T) {
		withConfig(t, &config.Config{})

		lines := signInLines()
		if len(lines) != 1 || !strings.Contains(lines[0], "none") {
			t.Errorf("got %q, want a single line saying none", lines)
		}
	})
}

func TestTokenMigrationDue(t *testing.T) {
	command := func(name string) *cobra.Command { return &cobra.Command{Use: name} }

	t.Run("said after a command that used the old token", func(t *testing.T) {
		withConfig(t, &config.Config{AccessToken: "jwt"})
		if !tokenMigrationDue(command("sync")) {
			t.Error("no reminder after a command that ran on a pre-v3 token")
		}
	})

	// One is where this is put right and the other reports it in full.
	for _, name := range []string{"login", "doctor"} {
		t.Run("not repeated on "+name, func(t *testing.T) {
			withConfig(t, &config.Config{AccessToken: "jwt"})
			if tokenMigrationDue(command(name)) {
				t.Errorf("reminder repeated on %s", name)
			}
		})
	}

	t.Run("never said to a session", func(t *testing.T) {
		withConfig(t, &config.Config{Session: &api.Session{UserToken: "ut"}})
		if tokenMigrationDue(command("sync")) {
			t.Error("reminder printed for a current session")
		}
	})

	t.Run("silent when the config never loaded", func(t *testing.T) {
		withConfig(t, nil)
		if tokenMigrationDue(command("sync")) {
			t.Error("reminder printed with no config")
		}
	})
}
