package modal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/progress"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

// DefaultEndpoint is the shared service. It is built in because using it
// should need nothing but a Google account: no URL to be told, no credential
// from the cloud it happens to run on.
const DefaultEndpoint = "https://jaisonerick--modal-whisper.modal.run"

// HTTPClient handles communication with the Whisper service.
type HTTPClient struct {
	EndpointURL string
	HTTP        *http.Client

	// Token proves who is calling. It is a function because the identity is
	// refreshed per run, and because nothing here should hold a credential.
	Token func(ctx context.Context) (string, error)
}

// authorize puts the caller's identity on a request.
func (c *HTTPClient) authorize(ctx context.Context, req *http.Request) error {
	if c.Token == nil {
		return fmt.Errorf("not signed in — run 'plaud auth login'")
	}
	token, err := c.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// NewHTTPClient points at the service, letting the environment or saved config
// override where that is.
func NewHTTPClient(savedEndpoint string, token func(ctx context.Context) (string, error)) *HTTPClient {
	endpoint := os.Getenv("PLAUD_WHISPER_URL")
	if endpoint == "" {
		endpoint = savedEndpoint
	}
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &HTTPClient{EndpointURL: endpoint, HTTP: &http.Client{}, Token: token}
}

// TranscribeOpts holds options for the transcription request.
type TranscribeOpts struct {
	Diarize            bool    `json:"diarize"`
	Polish             bool    `json:"polish"`
	Compact            bool    `json:"compact"`
	CompactGap         int     `json:"compact_gap"`
	Language           string  `json:"language,omitempty"`
	ContextDoc         string  `json:"context_doc,omitempty"`
	RecordingID        string  `json:"recording_id,omitempty"`
	SpeakerRecognition bool    `json:"speaker_recognition"`
	SpeakerThreshold   float64 `json:"speaker_threshold,omitempty"`
}

// TranscribeResult holds the structured response from a transcription.
type TranscribeResult struct {
	AudioID  string               `json:"audio_id"`
	Segments []transcript.Segment `json:"segments"`
	Speakers map[string]string    `json:"speakers"`
}

// SSEEvent represents a parsed server-sent event.
type SSEEvent struct {
	Type     string              `json:"type"` // "init", "update", "result", "error"
	Stages   []progress.StageDef `json:"stages,omitempty"`
	Stage    string              `json:"stage,omitempty"`
	Status   string              `json:"status,omitempty"`
	Detail   *string             `json:"detail,omitempty"`
	Progress *SSEProgress        `json:"progress,omitempty"`
	// Result fields (embedded when type == "result")
	AudioID  string               `json:"audio_id,omitempty"`
	Segments []transcript.Segment `json:"segments,omitempty"`
	Speakers map[string]string    `json:"speakers,omitempty"`
	// Error fields
	Message string `json:"message,omitempty"`
}

// Result is the finished transcription an event of type "result" carries.
// It exists so that every field the server sends is copied in one place: a
// field added to the wire and forgotten here goes missing without a sound.
func (e SSEEvent) Result() *TranscribeResult {
	return &TranscribeResult{
		AudioID:  e.AudioID,
		Segments: e.Segments,
		Speakers: e.Speakers,
	}
}

// SSEProgress represents a progress counter.
type SSEProgress struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

// StreamCallbacks provides hooks for upload progress monitoring.
type StreamCallbacks struct {
	OnUploadStart    func()
	OnUploadProgress func(sent, total int64)
}

// countingReader wraps an io.Reader and detects first read + progress.
type countingReader struct {
	r          io.Reader
	total      int64
	sent       int64
	firstRead  bool
	onFirst    func()
	onProgress func(sent, total int64)
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 && !cr.firstRead {
		cr.firstRead = true
		if cr.onFirst != nil {
			cr.onFirst()
		}
	}
	cr.sent += int64(n)
	if n > 0 && cr.onProgress != nil {
		cr.onProgress(cr.sent, cr.total)
	}
	return n, err
}

// TranscribeStream sends audio + options to the server and returns a channel
// of SSE events. The caller iterates events like a generator.
func (c *HTTPClient) TranscribeStream(ctx context.Context, audioData []byte, opts TranscribeOpts, callbacks StreamCallbacks) (<-chan SSEEvent, <-chan error) {
	events := make(chan SSEEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errCh)

		// Build multipart body
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// Audio part
		audioPart, err := writer.CreateFormFile("audio", "audio.mp3")
		if err != nil {
			errCh <- fmt.Errorf("creating audio form field: %w", err)
			return
		}
		if _, err := audioPart.Write(audioData); err != nil {
			errCh <- fmt.Errorf("writing audio data: %w", err)
			return
		}

		// Options part
		optsJSON, err := json.Marshal(opts)
		if err != nil {
			errCh <- fmt.Errorf("marshaling options: %w", err)
			return
		}
		if err := writer.WriteField("options", string(optsJSON)); err != nil {
			errCh <- fmt.Errorf("writing options field: %w", err)
			return
		}

		if err := writer.Close(); err != nil {
			errCh <- fmt.Errorf("closing multipart writer: %w", err)
			return
		}

		bodyBytes := buf.Bytes()
		bodySize := int64(len(bodyBytes))

		// Wrap body with counting reader for upload progress
		body := &countingReader{
			r:     bytes.NewReader(bodyBytes),
			total: bodySize,
			onFirst: func() {
				if callbacks.OnUploadStart != nil {
					callbacks.OnUploadStart()
				}
			},
			onProgress: func(sent, total int64) {
				if callbacks.OnUploadProgress != nil {
					callbacks.OnUploadProgress(sent, total)
				}
			},
		}

		url := strings.TrimRight(c.EndpointURL, "/") + "/transcribe?stream=true"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			errCh <- fmt.Errorf("creating request: %w", err)
			return
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if err := c.authorize(ctx, req); err != nil {
			errCh <- err
			return
		}
		req.ContentLength = bodySize

		resp, err := c.HTTP.Do(req)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "connection refused") {
				errCh <- fmt.Errorf("server is not reachable — the container may be down. Try again in a few minutes")
				return
			}
			if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
				errCh <- fmt.Errorf("request timed out — the server may be overloaded. Try again later")
				return
			}
			errCh <- fmt.Errorf("sending request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("the service refused the sign-in: %s", detailOf(body))
			return
		}
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			errCh <- classifyServerError(resp.StatusCode, respBody)
			return
		}

		// Parse SSE stream
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")

			var evt SSEEvent
			if err := json.Unmarshal([]byte(payload), &evt); err != nil {
				errCh <- fmt.Errorf("parsing SSE event: %w", err)
				return
			}

			select {
			case events <- evt:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "eof") ||
				strings.Contains(errStr, "internal_error") ||
				strings.Contains(errStr, "stream error") {
				errCh <- fmt.Errorf("server connection lost — the container likely crashed (possibly GPU out of memory). Try again in a few minutes")
				return
			}
			errCh <- fmt.Errorf("reading SSE stream: %w", err)
		}
	}()

	return events, errCh
}

// detailOf pulls the explanation out of the service's error body.
func detailOf(body []byte) string {
	var payload struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Detail != "" {
		return payload.Detail
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	if text == "" {
		return "no explanation given"
	}
	return text
}

// classifyServerError turns raw HTTP error responses into actionable messages.
func classifyServerError(status int, body []byte) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("the service refused the sign-in: %s", detailOf(body))
	}
	// The service explains a refusal in words meant for whoever typed the
	// command; repeating the status code over them helps nobody.
	if status == http.StatusNotFound || status == http.StatusBadRequest {
		return fmt.Errorf("%s", detailOf(body))
	}

	bodyStr := strings.ToLower(string(body))

	// Detect CUDA/GPU out-of-memory errors
	if strings.Contains(bodyStr, "cuda") && strings.Contains(bodyStr, "out of memory") ||
		strings.Contains(bodyStr, "cuda out of memory") ||
		strings.Contains(bodyStr, "torch.outofmemoryerror") {
		return fmt.Errorf("server GPU out of memory (CUDA OOM). The container may be overloaded — try again in a few minutes")
	}

	// Detect container crashes / restarts
	if strings.Contains(bodyStr, "container") && (strings.Contains(bodyStr, "killed") || strings.Contains(bodyStr, "oom") || strings.Contains(bodyStr, "crashed")) {
		return fmt.Errorf("server container crashed (likely out of memory). Try again in a few minutes")
	}

	switch status {
	case http.StatusBadGateway:
		return fmt.Errorf("server unavailable (502 Bad Gateway). The container may be starting up or crashed — try again in a minute")
	case http.StatusServiceUnavailable:
		return fmt.Errorf("server unavailable (503). The container may be scaling up — try again in a minute")
	case http.StatusGatewayTimeout:
		return fmt.Errorf("server timed out (504). The audio may be too long or the container is overloaded — try again later")
	case http.StatusInternalServerError:
		// Try to extract a meaningful message from the body
		if len(body) > 0 {
			// Try JSON error response
			var errResp struct {
				Detail string `json:"detail"`
				Error  string `json:"error"`
			}
			if json.Unmarshal(body, &errResp) == nil {
				msg := errResp.Detail
				if msg == "" {
					msg = errResp.Error
				}
				if msg != "" {
					return fmt.Errorf("server error (500): %s", msg)
				}
			}
			// Truncate raw body if too long
			raw := string(body)
			if len(raw) > 200 {
				raw = raw[:200] + "..."
			}
			return fmt.Errorf("server error (500): %s", raw)
		}
		return fmt.Errorf("server error (500). The container may have crashed — try again in a minute")
	default:
		raw := string(body)
		if len(raw) > 200 {
			raw = raw[:200] + "..."
		}
		return fmt.Errorf("server returned status %d: %s", status, raw)
	}
}
