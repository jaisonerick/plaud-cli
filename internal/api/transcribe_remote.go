package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TransStatus is returned by GET /ai/trans-status. It reports the account's
// transcription quota (in seconds) and how much of a job is currently running.
type TransStatus struct {
	Envelope
	TotalSeconds float64 `json:"total_seconds"` // seconds currently in-flight/consuming
	RemainTotal  float64 `json:"remain_total"`  // seconds of transcription quota remaining
}

// GetTransStatus returns the account transcription quota/status.
func (c *Client) GetTransStatus(ctx context.Context) (*TransStatus, error) {
	var resp TransStatus
	if err := c.Do(ctx, "GET", "/ai/trans-status", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// transcribeInfo is the transcription config carried, JSON-encoded, in the
// "info" field of the transsumm request. Keys left empty are omitted, matching
// the web app (which strips undefined values before sending).
type transcribeInfo struct {
	Language    string  `json:"language"`              // e.g. "auto", "pt", "en"
	Timezone    float64 `json:"timezone"`              // UTC offset in hours, e.g. -3
	Diarization int     `json:"diarization,omitempty"` // 1 = separate speakers
	LLM         string  `json:"llm,omitempty"`         // transcription model; "auto" lets Plaud choose
}

// autoSelectTemplate is the sentinel summ_type used by the web app's "Auto
// generation" flow — Plaud picks the best summary template itself. The endpoint
// rejects the request (status -12 "start trans task error") if summ_type is
// missing, so a value is always required.
const autoSelectTemplate = "AUTO-SELECT"

// transSummRequest is the body of POST /ai/transsumm/{id}, the call the web app
// makes when you click "Generate" on a recording. Plaud's remote trigger always
// produces a transcript together with a summary; there is no transcript-only
// variant (the web app's minimal path, "Auto generation", generates both).
type transSummRequest struct {
	IsReload       int    `json:"is_reload"`
	SummType       string `json:"summ_type"`      // template id, or "AUTO-SELECT"
	SummTypeType   string `json:"summ_type_type"` // "system" for AUTO-SELECT
	Info           string `json:"info"`           // JSON-encoded transcribeInfo
	SupportMulSumm bool   `json:"support_mul_summ"`
}

// TransSummResponse captures the transsumm reply. The body carries task info we
// don't need for polling (we poll GetDetail instead), so Data is kept raw.
type TransSummResponse struct {
	Envelope
	Data json.RawMessage `json:"data"`
}

// TranscribeOptions configures a remote transcription request.
type TranscribeOptions struct {
	Language    string // "auto" (default), "pt", "en", ... ; empty => "auto"
	Diarization bool   // separate speakers
	Reload      bool   // re-transcribe a recording that already has a transcript
	SummType    string // summary template id; empty => "AUTO-SELECT" (Plaud picks)
}

// localTimezoneHours returns the machine's UTC offset in hours, matching the
// web app's `-new Date().getTimezoneOffset()/60`.
func localTimezoneHours() float64 {
	_, offsetSec := time.Now().Zone()
	return float64(offsetSec) / 3600.0
}

// StartTranscription triggers transcription (and optionally summary generation)
// for a recording on Plaud's side — the remote "generate" action. The work runs
// asynchronously; poll GetDetail(id).HasTranscript() to observe completion.
func (c *Client) StartTranscription(ctx context.Context, id string, opts TranscribeOptions) (*TransSummResponse, error) {
	lang := opts.Language
	if lang == "" {
		lang = "auto"
	}
	info := transcribeInfo{Language: lang, Timezone: localTimezoneHours(), LLM: "auto"}
	if opts.Diarization {
		info.Diarization = 1
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("encoding transcribe info: %w", err)
	}

	summType := opts.SummType
	if summType == "" {
		summType = autoSelectTemplate
	}
	req := transSummRequest{
		IsReload:       boolToInt(opts.Reload),
		SummType:       summType,
		SummTypeType:   "system",
		Info:           string(infoJSON),
		SupportMulSumm: true,
	}

	var resp TransSummResponse
	if err := c.Do(ctx, "POST", "/ai/transsumm/"+id, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
