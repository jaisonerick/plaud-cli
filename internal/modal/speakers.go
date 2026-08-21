package modal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Person is somebody the service recognises.
type Person struct {
	Name      string `json:"name"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	CreatedBy string `json:"created_by"`
	Voices    int    `json:"voices"`
}

// Display is how a person is written wherever anyone reads them.
func (p Person) Display() string {
	return fmt.Sprintf("%s (%s)", p.Name, p.Company)
}

// SpeakerRanges are the stretches of one recording where a named person speaks,
// in milliseconds.
type SpeakerRanges struct {
	Name           string   `json:"name"`
	Company        string   `json:"company"`
	SurnameUnknown bool     `json:"surname_unknown,omitempty"`
	Ranges         [][2]int `json:"ranges"`
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
	if err := c.authorize(ctx, req); err != nil {
		return nil, err
	}
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

// NameSpeaker gives a diarized voice a person, creating that person if needed.
func (c *HTTPClient) NameSpeaker(ctx context.Context, audioID, speakerID, name, company string, surnameUnknown bool) (*Person, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for field, value := range map[string]string{
		"name": name, "company": company,
		"surname_unknown": strconv.FormatBool(surnameUnknown),
	} {
		if err := writer.WriteField(field, value); err != nil {
			return nil, fmt.Errorf("writing %s field: %w", field, err)
		}
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPut, fmt.Sprintf("/speakers/%s/%s", audioID, speakerID), writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}

	var result struct {
		Person Person `json:"person"`
		Voices int    `json:"voices"`
	}
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	result.Person.Voices = result.Voices
	return &result.Person, nil
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

// RenamePerson corrects who somebody is, carrying their voices across.
func (c *HTTPClient) RenamePerson(ctx context.Context, old, name, company string, surnameUnknown bool) (*Person, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for field, value := range map[string]string{
		"old": old, "new": name, "company": company,
		"surname_unknown": strconv.FormatBool(surnameUnknown),
	} {
		if err := writer.WriteField(field, value); err != nil {
			return nil, fmt.Errorf("writing %s field: %w", field, err)
		}
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPatch, "/speakers", writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}

	var result struct {
		Person Person `json:"person"`
	}
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result.Person, nil
}

// ForgetPerson drops a person and every voice of theirs.
func (c *HTTPClient) ForgetPerson(ctx context.Context, name string) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("name", name); err != nil {
		return fmt.Errorf("writing name field: %w", err)
	}
	writer.Close()

	req, err := c.request(ctx, http.MethodPost, "/speakers/forget", writer.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// VoiceMatch is who one voice of a recording is today. Name is empty when
// nobody known is close enough, and Known is false when the recording holds no
// such voice at all, which is what a transcript from a run since replaced asks.
type VoiceMatch struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Voice is the id of the voice that answered, which a transcript that
	// asked by a label writes down so it stops depending on one run's numbering.
	Voice    string   `json:"voice"`
	Distance *float64 `json:"distance"`
	Known    bool     `json:"known"`
}

// RecordingVoices is the labels this service holds for a recording, which is
// what says a transcript was made here: one made here never reaches Plaud, and
// Plaud's own record goes on saying the recording has none.
func (c *HTTPClient) RecordingVoices(ctx context.Context, recordingID string) ([]string, error) {
	req, err := c.request(ctx, http.MethodGet, "/speakers/"+url.PathEscape(recordingID), "", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Voices []string `json:"voices"`
	}
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return result.Voices, nil
}

// WhoIs asks who each voice of a recording is, by the ids a transcript kept, or
// by the labels one written before those ids carries.
func (c *HTTPClient) WhoIs(ctx context.Context, recordingID string, keys []string) ([]VoiceMatch, error) {
	wanted, err := json.Marshal(keys)
	if err != nil {
		return nil, fmt.Errorf("marshaling the keys: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("keys", string(wanted)); err != nil {
		return nil, fmt.Errorf("writing keys field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := c.request(ctx, http.MethodPost, "/speakers/"+url.PathEscape(recordingID)+"/whois", writer.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}

	var result struct {
		Voices []VoiceMatch `json:"voices"`
	}
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return result.Voices, nil
}

// ListPeople returns everybody the service recognises.
func (c *HTTPClient) ListPeople(ctx context.Context) ([]Person, error) {
	req, err := c.request(ctx, http.MethodGet, "/speakers", "", nil)
	if err != nil {
		return nil, err
	}

	var people []Person
	if err := c.do(req, &people); err != nil {
		return nil, err
	}
	return people, nil
}
