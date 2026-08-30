package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client wraps the Plaud API.
type Client struct {
	BaseURL  string
	Token    string
	DeviceID string
	Debug    bool
	HTTP     *http.Client

	// Session is the v3 cookie session, when there is one. Token is the older
	// bearer credential, which PLAUD_TOKEN and `login --token` still supply.
	// Both are sent when both are known: which one the server honours is its
	// business, and an account mid-migration may answer to either.
	Session *Session

	// OnSession is called whenever the session changes, which is at login and
	// again on every refresh. A rotated cookie that is never written down
	// leaves the next process to start from an expired one.
	OnSession func(*Session)
}

// sessionChanged folds a response's cookies into the session and persists it.
func (c *Client) sessionChanged(resp *http.Response) {
	if c.Session == nil {
		c.Session = &Session{}
	}
	if c.Session.readCookies(resp) && c.OnSession != nil {
		c.OnSession(c.Session)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) do(ctx context.Context, req *http.Request, result interface{}) error {
	return c.send(ctx, req, result, true)
}

// send performs one request, renewing the session and trying again when the
// answer is 401 and there is a refresh token to renew it with. mayRefresh is
// false on the retry and on the refresh call itself, so a server that keeps
// refusing ends the attempt rather than looping.
func (c *Client) send(ctx context.Context, req *http.Request, result interface{}, mayRefresh bool) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Session.Valid() {
		req.AddCookie(&http.Cookie{Name: cookieUserToken, Value: c.Session.UserToken})
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("app-language", "en")
	req.Header.Set("app-platform", "web")
	req.Header.Set("edit-from", "web")
	req.Header.Set("Origin", "https://web.plaud.ai")
	req.Header.Set("Referer", "https://web.plaud.ai/")
	req.Header.Set("x-request-id", randomHex(5))
	req.Header.Set("x-device-id", c.DeviceID)
	req.Header.Set("x-pld-tag", c.DeviceID)
	if tz := time.Now().Location().String(); strings.Contains(tz, "/") {
		// IANA zone name (e.g. America/Sao_Paulo), matching the web app's `timezone` header.
		req.Header.Set("timezone", tz)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if c.Debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] %s %s → %d\n", req.Method, req.URL, resp.StatusCode)
		// Which cookies a response set, never their values: a session cookie
		// is the credential itself, and --debug output is pasted into bug
		// reports. The name and whether it was set or cleared is what a
		// scheme change is diagnosed from.
		for _, ck := range resp.Cookies() {
			state := "cleared"
			if ck.Value != "" {
				state = fmt.Sprintf("set, %d bytes", len(ck.Value))
			}
			fmt.Fprintf(os.Stderr, "[DEBUG] Set-Cookie: %s (%s) Path=%s Domain=%s\n",
				ck.Name, state, ck.Path, ck.Domain)
		}
		fmt.Fprintf(os.Stderr, "%s\n", string(body))
	}

	c.sessionChanged(resp)

	if resp.StatusCode == 401 {
		if mayRefresh && c.Session.Renewable() {
			if err := c.refresh(ctx); err == nil {
				retry, rerr := rewind(req)
				if rerr != nil {
					return rerr
				}
				return c.send(ctx, retry, result, false)
			}
		}
		return &APIError{Status: 401, Msg: "Session expired. Run 'plaud login' again."}
	}

	if result == nil {
		return nil
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	// Check envelope status
	var env Envelope
	if err := json.Unmarshal(body, &env); err == nil && env.Status != 0 {
		return &APIError{Status: env.Status, Msg: env.Msg}
	}

	return nil
}

// Do sends a JSON request and decodes the response.
func (c *Client) Do(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	u := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.do(ctx, req, result)
}

// PostForm sends a form-encoded request and decodes the response.
func (c *Client) PostForm(ctx context.Context, path string, values url.Values, result interface{}) error {
	u := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return c.do(ctx, req, result)
}

// FetchFile downloads a URL and returns the raw bytes.
// If onProgress is non-nil, it is called with (bytesReceived, totalBytes)
// as data arrives. totalBytes is -1 if Content-Length is unknown.
func (c *Client) FetchFile(ctx context.Context, fileURL string, onProgress func(received, total int64)) ([]byte, error) {
	return withRetries(ctx, func() ([]byte, error) {
		return c.fetchOnce(ctx, fileURL, onProgress)
	})
}

// withRetries repeats fn while the failure still looks like the network rather
// than an answer.
func withRetries[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		value, err := fn()
		if err == nil {
			return value, nil
		}
		lastErr = err

		var status *statusError
		if ctx.Err() != nil || errors.As(err, &status) {
			break
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return zero, lastErr
}

// fetchAttempts is how often a download is tried before giving up. The signed
// URLs these come from drop connections mid-transfer often enough that one EOF
// should not cost a run that downloads dozens of files in a row.
const fetchAttempts = 3

// statusError is a refusal from the other end, which retrying only repeats.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("download returned status %d", e.code) }

func (c *Client) fetchOnce(ctx context.Context, fileURL string, onProgress func(received, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{code: resp.StatusCode}
	}

	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &progressReader{
			r:          resp.Body,
			total:      resp.ContentLength,
			onProgress: onProgress,
		}
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return data, nil
}

// progressReader wraps an io.Reader and reports progress.
type progressReader struct {
	r          io.Reader
	total      int64
	received   int64
	onProgress func(received, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.received += int64(n)
	if n > 0 {
		pr.onProgress(pr.received, pr.total)
	}
	return n, err
}

func (c *Client) DownloadFile(ctx context.Context, fileURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// rewind returns a fresh copy of a request whose body has already been read,
// so a call refused for a stale session can be made again once it is renewed.
// Requests built by Do and PostForm carry GetBody, because their bodies are
// readers the stdlib knows how to replay; one without it cannot be retried.
func rewind(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	clone.Header.Del("Cookie")

	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("the session was renewed but this request cannot be sent again")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("replaying the request after renewing the session: %w", err)
	}
	clone.Body = body
	return clone, nil
}

// refresh buys a new user token with the refresh one.
//
// The refresh cookie is scoped by the server to this route alone, so it is
// sent here and nowhere else; the new cookies arrive on the response and are
// picked up like any other.
func (c *Client) refresh(ctx context.Context) error {
	if !c.Session.Renewable() {
		return fmt.Errorf("no refresh token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+refreshPath, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("creating the refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieRefreshToken, Value: c.Session.RefreshToken})

	var resp SessionResponse
	if err := c.send(ctx, req, &resp, false); err != nil {
		return err
	}
	c.Session.take(resp)
	if c.OnSession != nil {
		c.OnSession(c.Session)
	}
	return nil
}
