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

// AuthResponse is returned by POST /auth/access-token.
type AuthResponse struct {
	Envelope
	Data struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	} `json:"data"`
}

// UserResponse is returned by GET /user/me.
type UserResponse struct {
	Envelope
	Data UserInfo `json:"data"`
}

type UserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	CreatedAt string `json:"created_at"`
}

// TagListResponse is returned by GET /filetag/.
type TagListResponse struct {
	Envelope
	Data []Tag `json:"data"`
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RecordingListResponse is returned by GET /file/simple/web.
type RecordingListResponse struct {
	Envelope
	Data []RecordingSimple `json:"data"`
}

type RecordingSimple struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Duration    int      `json:"duration"`
	CreatedAt   string   `json:"created_at"`
	Tags        []string `json:"tags"`
	HasSummary  bool     `json:"has_ai_summary"`
	HasTranscript bool   `json:"has_ai_transcript"`
	FileType    string   `json:"file_type"`
}

// FormatDuration returns a human-readable duration string.
func FormatDuration(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

// RecordingDetailResponse is returned by GET /file/detail/{id}.
type RecordingDetailResponse struct {
	Envelope
	Data RecordingDetail `json:"data"`
}

type RecordingDetail struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Duration      int    `json:"duration"`
	CreatedAt     string `json:"created_at"`
	FileType      string `json:"file_type"`
	FilePath      string `json:"file_path"`
	Tags          []Tag  `json:"tags"`
	HasSummary    bool   `json:"has_ai_summary"`
	HasTranscript bool   `json:"has_ai_transcript"`
	Transcript    string `json:"ai_transcript"`
	Summary       string `json:"ai_summary"`
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
	Data struct {
		URL string `json:"url"`
	} `json:"data"`
}

// ExportRequest is sent to POST /file/document/export.
type ExportRequest struct {
	FileID   string `json:"file_id"`
	Type     string `json:"type"`     // "transcript" or "summary"
	Format   string `json:"format"`   // "txt", "srt", "md", "docx", "pdf"
}

// ExportResponse is returned by POST /file/document/export.
type ExportResponse struct {
	Envelope
	Data struct {
		URL string `json:"url"`
	} `json:"data"`
}

// UploadInfoRequest is sent to POST /others/upload-info.
type UploadInfoRequest struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}
