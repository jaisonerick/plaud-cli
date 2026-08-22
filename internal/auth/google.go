// Package auth signs in to the Whisper service with a Google account, so that
// using it needs no credential from the cloud it happens to run on.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the sign-in the service asks callers to use. It comes from the
// service so that nothing here carries an OAuth client of its own.
type Config struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	Scopes       []string `json:"scopes"`
	Domains      []string `json:"domains"`
}

// DeviceCode is a pending sign-in: a short code for a person to type somewhere
// else, and a handle for this machine to ask about it with.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Session is what survives between runs: enough to ask Google for a fresh
// token without opening a browser again.
type Session struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refresh_token"`
}

// FetchConfig reads the sign-in details the service publishes.
func FetchConfig(ctx context.Context, serviceURL string) (*Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(serviceURL, "/")+"/auth/config", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking %s how to sign in: %w", serviceURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s did not return sign-in details (status %d)", serviceURL, resp.StatusCode)
	}
	var cfg Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing sign-in details: %w", err)
	}
	if cfg.ClientID == "" || cfg.TokenURI == "" || cfg.AuthURI == "" {
		return nil, fmt.Errorf("%s returned incomplete sign-in details", serviceURL)
	}
	return &cfg, nil
}

// DeviceEndpoint is where a sign-in with nowhere to redirect to begins.
const DeviceEndpoint = "https://oauth2.googleapis.com/device/code"

// StartDevice asks Google for a code the person can type on any device.
// Nothing here needs a browser, a port, or a terminal anybody can see, which
// is what lets the sign-in be conducted by whoever is running the commands.
func StartDevice(ctx context.Context, cfg *Config) (*DeviceCode, error) {
	form := url.Values{
		"client_id": {cfg.ClientID},
		"scope":     {strings.Join(scopesOf(cfg), " ")},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DeviceEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking Google to start the sign-in: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var pending DeviceCode
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, fmt.Errorf("parsing Google's answer: %w", err)
	}
	if pending.UserCode == "" {
		var failure tokenResponse
		_ = json.Unmarshal(data, &failure)
		return nil, fmt.Errorf("Google refused to start the sign-in: %s",
			strings.TrimSpace(failure.Error+" "+failure.Description))
	}
	if pending.Interval <= 0 {
		pending.Interval = 5
	}
	return &pending, nil
}

// AwaitDevice waits for the person to finish, and returns the session.
func AwaitDevice(ctx context.Context, cfg *Config, pending *DeviceCode) (*Session, error) {
	deadline := time.Now().Add(time.Duration(pending.ExpiresIn) * time.Second)
	wait := time.Duration(pending.Interval) * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the code expired before it was entered — start again")
		}

		body, err := exchange(ctx, cfg, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {pending.DeviceCode},
		})
		if err == nil {
			if body.RefreshToken == "" {
				return nil, fmt.Errorf("Google returned no refresh token, so this would ask again every time")
			}
			return &Session{Email: emailIn(body.IDToken), RefreshToken: body.RefreshToken}, nil
		}

		switch {
		case strings.Contains(err.Error(), "authorization_pending"):
			// Nobody has typed the code yet, which is the normal state.
		case strings.Contains(err.Error(), "slow_down"):
			wait += 5 * time.Second
		case strings.Contains(err.Error(), "access_denied"):
			return nil, fmt.Errorf("the sign-in was refused on the other device")
		case strings.Contains(err.Error(), "expired_token"):
			return nil, fmt.Errorf("the code expired before it was entered — start again")
		default:
			return nil, err
		}
	}
}

func scopesOf(cfg *Config) []string {
	if len(cfg.Scopes) > 0 {
		return cfg.Scopes
	}
	return []string{"openid", "email", "profile"}
}

// SessionPath is where the sign-in is kept.
func SessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "plaud", "auth.json"), nil
}

// LoadSession reads the stored sign-in, returning nil when there is none.
func LoadSession() (*Session, error) {
	path, err := SessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &session, nil
}

// SaveSession stores the sign-in where only its owner can read it.
func SaveSession(session *Session) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	// A refresh token is worth the Google account until somebody revokes it.
	return os.WriteFile(path, data, 0600)
}

// ForgetSession removes the stored sign-in.
func ForgetSession() error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IDToken returns a token proving who the caller is, refreshing the stored
// sign-in. It returns an error rather than opening a browser: signing in is
// something a person asks for, not something a command does behind them.
func IDToken(ctx context.Context, cfg *Config, session *Session) (string, error) {
	if session == nil || session.RefreshToken == "" {
		return "", fmt.Errorf("not signed in — run 'plaud auth login'")
	}
	body, err := exchange(ctx, cfg, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {session.RefreshToken},
	})
	if err != nil {
		return "", fmt.Errorf("that sign-in no longer works, run 'plaud auth login' again: %w", err)
	}
	if body.IDToken == "" {
		return "", fmt.Errorf("Google returned no identity token — run 'plaud auth login' again")
	}
	return body.IDToken, nil
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func exchange(ctx context.Context, cfg *Config, form url.Values) (*tokenResponse, error) {
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var body tokenResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("parsing Google's answer: %w", err)
	}
	if body.Error != "" {
		return nil, fmt.Errorf("Google refused: %s", strings.TrimSpace(body.Error+" "+body.Description))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google answered %d", resp.StatusCode)
	}
	return &body, nil
}

// emailIn reads the address out of an ID token, for showing who signed in.
// Nothing is decided on this: the service checks the signature itself.
func emailIn(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Email
}
