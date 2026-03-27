package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Client wraps the Plaud API.
type Client struct {
	BaseURL  string
	Token    string
	DeviceID string
	Debug    bool
	HTTP     *http.Client
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) do(ctx context.Context, req *http.Request, result interface{}) error {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("app-language", "en")
	req.Header.Set("app-platform", "web")
	req.Header.Set("edit-from", "web")
	req.Header.Set("Origin", "https://web.plaud.ai")
	req.Header.Set("Referer", "https://web.plaud.ai/")
	req.Header.Set("x-request-id", randomHex(5))
	req.Header.Set("x-device-id", c.DeviceID)
	req.Header.Set("x-pld-tag", c.DeviceID)

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
		fmt.Fprintf(os.Stderr, "[DEBUG] %s %s → %d\n%s\n", req.Method, req.URL, resp.StatusCode, string(body))
	}

	if resp.StatusCode == 401 {
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
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
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
