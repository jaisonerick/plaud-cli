package api

import (
	"net/http"
	"time"
)

// The v3 session cookies. `pld_ut` authenticates every request; `pld_urt`
// buys a new one, and the server scopes it to the refresh route alone, so it
// is only ever sent there.
const (
	cookieUserToken    = "pld_ut"
	cookieRefreshToken = "pld_urt"
	refreshPath        = "/auth/refresh-user-token"
)

// Session is what the v3 API hands back instead of a bearer token.
//
// `access_token` in the body is deliberately empty from v3 onwards: the
// credential arrives as an httpOnly cookie and the body carries only a
// `token_id`, which identifies the session without being usable as one. A
// client that reads `access_token` and ignores Set-Cookie therefore stores an
// empty string, reports success, and is refused by every later call.
type Session struct {
	TokenID          string `json:"token_id,omitempty"`
	UserToken        string `json:"user_token,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	RefreshExpiresAt int64  `json:"refresh_expires_at,omitempty"`
}

// Valid reports whether this session still carries something to authenticate
// with. An expiry in the past is still valid here: the refresh route is what
// settles that, and the clock on this machine is not the server's.
func (s *Session) Valid() bool {
	return s != nil && s.UserToken != ""
}

// Renewable reports whether a refused session can be bought back.
func (s *Session) Renewable() bool {
	return s != nil && s.RefreshToken != ""
}

// Expiry returns when the user token stops being accepted, or nil when the
// server did not say.
func (s *Session) Expiry() *time.Time {
	if s == nil || s.ExpiresAt == 0 {
		return nil
	}
	at := time.Unix(s.ExpiresAt, 0)
	return &at
}

// readCookies folds the cookies of one response into the session, reporting
// whether anything changed so the caller knows to persist it.
//
// A cookie cleared to the empty string is ignored rather than stored. Signing
// in sets both cookies blank across every domain and path it might previously
// have used before setting the real ones, and taking those at face value would
// wipe the session that the same response is establishing.
func (s *Session) readCookies(resp *http.Response) bool {
	changed := false
	for _, c := range resp.Cookies() {
		if c.Value == "" {
			continue
		}
		switch c.Name {
		case cookieUserToken:
			if s.UserToken != c.Value {
				s.UserToken = c.Value
				changed = true
			}
		case cookieRefreshToken:
			if s.RefreshToken != c.Value {
				s.RefreshToken = c.Value
				changed = true
			}
		}
	}
	return changed
}

// SessionResponse is what every route that establishes a session returns:
// /auth/otp-login, /auth/access-token and /auth/refresh-user-token.
//
// The token fields are empty from v3 onwards and the expiries are not, which
// is the clearest signal of which scheme answered.
type SessionResponse struct {
	Envelope
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	TokenID      string `json:"token_id"`
	VersionTag   string `json:"version_tag"`
	UID          string `json:"uid"`
	UTExpireAt   int64  `json:"ut_expire_at"`
	URTExpireAt  int64  `json:"urt_expire_at"`
	IsNewUser    bool   `json:"is_new_user"`
}

// take folds the body of a session response into the session. The cookies are
// read separately, off the response itself; what is here is what only the body
// says — the session's identity and when it lapses — plus the tokens as the
// older scheme returned them, for a server still answering that way.
func (s *Session) take(resp SessionResponse) {
	if resp.TokenID != "" {
		s.TokenID = resp.TokenID
	}
	if resp.AccessToken != "" {
		s.UserToken = resp.AccessToken
	}
	if resp.RefreshToken != "" {
		s.RefreshToken = resp.RefreshToken
	}
	if resp.UTExpireAt != 0 {
		s.ExpiresAt = resp.UTExpireAt
	}
	if resp.URTExpireAt != 0 {
		s.RefreshExpiresAt = resp.URTExpireAt
	}
}
