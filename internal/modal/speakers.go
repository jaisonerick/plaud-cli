package modal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// KnownSpeaker is a voice the server recognises, and how many samples back it.
type KnownSpeaker struct {
	Name    string `json:"name"`
	Samples int    `json:"samples"`
}

// SpeakerRanges are the stretches of one recording where a named person speaks,
// in milliseconds.
type SpeakerRanges struct {
	Name   string   `json:"name"`
	Ranges [][2]int `json:"ranges"`
}

// EnrollResult reports what an enrollment made of each name it was given.
type EnrollResult struct {
	Enrolled map[string]int    `json:"enrolled"`
	Skipped  map[string]string `json:"skipped"`
}

func (c *HTTPClient) request(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.EndpointURL, "/")+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Modal-Key", c.TokenID)
	req.Header.Set("Modal-Secret", c.TokenSecret)
	return req, nil
}

func (c *HTTPClient) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return classifyServerError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	return nil
}

// SetSpeakerName names a speaker the server still holds the embedding for.
func (c *HTTPClient) SetSpeakerName(ctx context.Context, audioID, speakerID, name string) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("name", name); err != nil {
		return fmt.Errorf("writing name field: %w", err)
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPut, fmt.Sprintf("/speakers/%s/%s", audioID, speakerID), writer.FormDataContentType(), &buf)
	if err != nil {
		return err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("the server no longer holds an embedding for %s/%s — name it from the transcript instead", audioID, speakerID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return classifyServerError(resp.StatusCode, body)
	}
	return nil
}

// AddKnownSpeaker names a voice from an embedding the caller already holds,
// which is what a transcript carries once it has been saved.
func (c *HTTPClient) AddKnownSpeaker(ctx context.Context, name string, embedding []float64) (int, error) {
	vector, err := json.Marshal(embedding)
	if err != nil {
		return 0, fmt.Errorf("marshaling embedding: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("name", name); err != nil {
		return 0, fmt.Errorf("writing name field: %w", err)
	}
	if err := writer.WriteField("embedding", string(vector)); err != nil {
		return 0, fmt.Errorf("writing embedding field: %w", err)
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPost, "/speakers", writer.FormDataContentType(), &buf)
	if err != nil {
		return 0, err
	}

	var result KnownSpeaker
	if err := c.do(req, &result); err != nil {
		return 0, err
	}
	return result.Samples, nil
}

// EnrollSpeakers registers voices from a recording somebody already attributed,
// letting the server cut the named stretches out of the audio itself.
func (c *HTTPClient) EnrollSpeakers(ctx context.Context, audioData []byte, speakers []SpeakerRanges) (*EnrollResult, error) {
	spec, err := json.Marshal(speakers)
	if err != nil {
		return nil, fmt.Errorf("marshaling speaker ranges: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	audioPart, err := writer.CreateFormFile("audio", "audio.mp3")
	if err != nil {
		return nil, fmt.Errorf("creating audio form field: %w", err)
	}
	if _, err := audioPart.Write(audioData); err != nil {
		return nil, fmt.Errorf("writing audio data: %w", err)
	}
	if err := writer.WriteField("speakers", string(spec)); err != nil {
		return nil, fmt.Errorf("writing speakers field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := c.request(ctx, http.MethodPost, "/speakers/enroll", writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(buf.Len())

	var result EnrollResult
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RenameKnownSpeaker moves every sample of one spelling onto another.
func (c *HTTPClient) RenameKnownSpeaker(ctx context.Context, old, new string) (int, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("old", old); err != nil {
		return 0, fmt.Errorf("writing old name field: %w", err)
	}
	if err := writer.WriteField("new", new); err != nil {
		return 0, fmt.Errorf("writing new name field: %w", err)
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPatch, "/speakers", writer.FormDataContentType(), &buf)
	if err != nil {
		return 0, err
	}

	var result struct {
		Moved int `json:"moved"`
	}
	if err := c.do(req, &result); err != nil {
		return 0, err
	}
	return result.Moved, nil
}

// ListKnownSpeakers returns every voice the server recognises.
func (c *HTTPClient) ListKnownSpeakers(ctx context.Context) ([]KnownSpeaker, error) {
	req, err := c.request(ctx, http.MethodGet, "/speakers", "", nil)
	if err != nil {
		return nil, err
	}

	var speakers []KnownSpeaker
	if err := c.do(req, &speakers); err != nil {
		return nil, err
	}
	return speakers, nil
}
