package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Signing in clears both cookies across every domain and path it might
// previously have used, and only then sets the real ones. Reading those clears
// as logouts would throw away the session the same response is establishing —
// which is the shape of the bug this guards.
func TestReadCookiesIgnoresTheClearsThatPrecedeALogin(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	for _, c := range []string{
		"pld_ut=; Path=/; Domain=plaud.ai; Max-Age=0",
		"pld_urt=; Path=/auth/refresh-user-token; Domain=plaud.ai; Max-Age=0",
		"pld_ut=; Path=/; Max-Age=0",
		"pld_ut=the-real-one; Path=/; Domain=plaud.ai; Max-Age=86400",
		"pld_urt=the-real-refresh; Path=/auth/refresh-user-token; Max-Age=2592000",
	} {
		resp.Header.Add("Set-Cookie", c)
	}

	var s Session
	if !s.readCookies(resp) {
		t.Fatal("a response carrying both cookies reported no change")
	}
	if s.UserToken != "the-real-one" {
		t.Errorf("user token = %q, want the-real-one", s.UserToken)
	}
	if s.RefreshToken != "the-real-refresh" {
		t.Errorf("refresh token = %q, want the-real-refresh", s.RefreshToken)
	}
}

// A v3 login answers "success" with every token field empty. Reading the body
// alone stores nothing and reports a login that did not happen.
func TestEstablishedRefusesASessionItCannotAuthenticateWith(t *testing.T) {
	c := &Client{}
	_, err := c.established(SessionResponse{TokenID: "abc", VersionTag: "v3"})
	if err == nil {
		t.Fatal("a response with no token and no cookie was accepted as a login")
	}
}

// The session is what makes the call, and a refused one is renewed and the
// call made again — once, with the body intact.
func TestSendRenewsARefusedSessionAndRepeatsTheCall(t *testing.T) {
	var refreshes, calls int
	var sawBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == refreshPath {
			refreshes++
			if _, err := r.Cookie(cookieRefreshToken); err != nil {
				t.Errorf("the refresh call carried no %s cookie", cookieRefreshToken)
			}
			http.SetCookie(w, &http.Cookie{Name: cookieUserToken, Value: "renewed"})
			w.Write([]byte(`{"status":0,"token_id":"t2","ut_expire_at":123}`))
			return
		}

		calls++
		ut, err := r.Cookie(cookieUserToken)
		if err != nil {
			t.Errorf("call %d carried no %s cookie", calls, cookieUserToken)
			w.WriteHeader(401)
			return
		}
		if ut.Value == "stale" {
			w.WriteHeader(401)
			return
		}
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		sawBody = string(buf)
		w.Write([]byte(`{"status":0}`))
	}))
	defer srv.Close()

	var saved *Session
	c := &Client{
		BaseURL:   srv.URL,
		HTTP:      srv.Client(),
		Session:   &Session{UserToken: "stale", RefreshToken: "refresh-me"},
		OnSession: func(s *Session) { saved = s },
	}

	var out Envelope
	if err := c.Do(context.Background(), "POST", "/user/me", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatalf("call failed after renewal: %v", err)
	}

	if refreshes != 1 {
		t.Errorf("refreshed %d times, want 1", refreshes)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2 (the refused one and the repeat)", calls)
	}
	if sawBody != `{"a":"b"}` {
		t.Errorf("the repeated call sent %q, want the original body", sawBody)
	}
	if saved == nil || saved.UserToken != "renewed" {
		t.Error("the renewed session was never handed over to be saved")
	}
}

// A server that goes on refusing ends the attempt rather than looping.
func TestSendGivesUpWhenRenewalDoesNotHelp(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == refreshPath {
			http.SetCookie(w, &http.Cookie{Name: cookieUserToken, Value: "still-no-good"})
			w.Write([]byte(`{"status":0}`))
			return
		}
		calls++
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := &Client{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
		Session: &Session{UserToken: "stale", RefreshToken: "refresh-me"},
	}

	err := c.Do(context.Background(), "GET", "/user/me", nil, &Envelope{})
	if err == nil {
		t.Fatal("a call refused twice reported success")
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2: one refusal, one retry, then stop", calls)
	}
}

// A login that establishes nothing must not be waved through on the strength
// of the session it was meant to replace. Signing in as somebody else, or as
// an address with no account, otherwise reports success while every later call
// still answers as the account already signed in.
func TestALoginCannotBeVouchedForByTheSessionItReplaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Set-Cookie: the server took the code and handed over nothing.
		w.Write([]byte(`{"status":0,"msg":"success","access_token":"","token_id":"t","version_tag":"v3"}`))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
		Session: &Session{UserToken: "the-account-already-signed-in", RefreshToken: "r"},
	}

	if _, err := c.VerifyCode(context.Background(), "otp", "123456"); err == nil {
		t.Fatal("a login that established nothing was accepted")
	}
	if c.Session.Valid() {
		t.Error("the session that the failed login was meant to replace is still in hand")
	}
}

// An address nobody has an account for is signed up rather than refused, so a
// typo answers "success". That reads as an empty account rather than a wrong
// address unless the client says which happened.
func TestANewAccountIsReportedAsSuchRatherThanAsAnEmptyOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":0,"msg":"success","access_token":"","is_new_user":true,"set_password_token":"x"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	_, err := c.VerifyCode(context.Background(), "otp", "123456")
	if err == nil {
		t.Fatal("signing up a brand new account was reported as a successful login")
	}
	if !strings.Contains(err.Error(), "just created") {
		t.Errorf("the error does not say an account was created: %v", err)
	}
}
