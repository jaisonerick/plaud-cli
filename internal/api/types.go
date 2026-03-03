package api

import (
	"fmt"
	"time"
)

// Envelope is the standard API response wrapper.
type Envelope struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// APIError represents a non-zero status from the API.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return "api error"
}

// IsTokenExpired returns true if this error indicates an expired/invalid session.
func (e *APIError) IsTokenExpired() bool {
	return e.Status == 401 || e.Status == 40101
}

// UserResponse is returned by GET /user/me.
type UserResponse struct {
	Envelope
	Data  UserInfo  `json:"data_user"`
	State UserState `json:"data_state"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Country  string `json:"country"`
}

type UserState struct {
	CreatedAt int64 `json:"created_at"` // epoch seconds
}

// TagListResponse is returned by GET /filetag/.
type TagListResponse struct {
	Envelope
	Total int   `json:"data_filetag_total"`
	Data  []Tag `json:"data_filetag_list"`
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RecordingListResponse is returned by GET /file/simple/web.
type RecordingListResponse struct {
	Envelope
	Total int               `json:"data_file_total"`
	Data  []RecordingSimple `json:"data_file_list"`
}

type RecordingSimple struct {
	ID            string   `json:"id"`
	Name          string   `json:"filename"`
	Duration      int64    `json:"duration"`      // milliseconds
	StartTime     int64    `json:"start_time"`     // epoch ms
	EditTime      int64    `json:"edit_time"`      // epoch seconds
	Tags          []string `json:"filetag_id_list"`
	HasSummary    bool     `json:"is_summary"`
	HasTranscript bool     `json:"is_trans"`
	FileType      string   `json:"filetype"`       // MIME type, e.g. "audio/mp3"
	Scene         int      `json:"scene"`
}

// FormatDurationMs returns a human-readable duration from milliseconds.
func FormatDurationMs(ms int64) string {
	seconds := int(ms / 1000)
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

// FormatEpochMs formats an epoch-millisecond timestamp as a readable date.
func FormatEpochMs(ms int64) string {
	t := time.Unix(0, ms*int64(time.Millisecond))
	return t.Format("2006-01-02 15:04")
}

// RecordingDetailResponse is returned by GET /file/detail/{id}.
type RecordingDetailResponse struct {
	Envelope
	Data RecordingDetail `json:"data"`
}

type RecordingDetail struct {
	ID          string        `json:"file_id"`
	Name        string        `json:"file_name"`
	Duration    int64         `json:"duration"`    // milliseconds
	StartTime   int64         `json:"start_time"`  // epoch ms
	Scene       int           `json:"scene"`
	Tags        []string      `json:"filetag_id_list"`
	ContentList []ContentItem `json:"content_list"`
}

// ContentItem represents a transcript, summary, or note in the detail response.
type ContentItem struct {
	DataID     string `json:"data_id"`
	DataType   string `json:"data_type"`   // "transaction" (transcript), "auto_sum_note" (summary), "consumer_note"
	TaskStatus int    `json:"task_status"` // 1 = ready
	DataTitle  string `json:"data_title"`
	DataLink   string `json:"data_link"`   // presigned S3 URL
}

// HasTranscript returns true if a transcript is available.
func (d *RecordingDetail) HasTranscript() bool {
	for _, c := range d.ContentList {
		if c.DataType == "transaction" && c.TaskStatus == 1 {
			return true
		}
	}
	return false
}

// HasSummary returns true if a summary is available.
func (d *RecordingDetail) HasSummary() bool {
	for _, c := range d.ContentList {
		if c.DataType == "auto_sum_note" && c.TaskStatus == 1 {
			return true
		}
	}
	return false
}

// TranscriptURL returns the presigned URL for the transcript, or empty string.
func (d *RecordingDetail) TranscriptURL() string {
	for _, c := range d.ContentList {
		if c.DataType == "transaction" && c.TaskStatus == 1 {
			return c.DataLink
		}
	}
	return ""
}

// SummaryURL returns the presigned URL for the summary, or empty string.
func (d *RecordingDetail) SummaryURL() string {
	for _, c := range d.ContentList {
		if c.DataType == "auto_sum_note" && c.TaskStatus == 1 {
			return c.DataLink
		}
	}
	return ""
}

// FormatDate parses a date string and returns a readable format.
func FormatDate(s string) string {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	}
	return s
}

// TempURLResponse is returned by GET /file/temp-url/{id}.
type TempURLResponse struct {
	Envelope
	URL string `json:"temp_url"`
}

