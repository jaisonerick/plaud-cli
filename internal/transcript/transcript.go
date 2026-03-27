package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Segment represents a single transcript segment from Plaud's JSON format.
type Segment struct {
	StartTime int64  `json:"start_time"` // milliseconds
	EndTime   int64  `json:"end_time"`   // milliseconds
	Content   string `json:"content"`
	Speaker   string `json:"speaker"`
}

// Match represents a search result within a transcript.
type Match struct {
	Segment Segment
	Index   int // index of the segment in the original slice
}

// Parse unmarshals transcript JSON (array of segments).
func Parse(data []byte) ([]Segment, error) {
	var segments []Segment
	if err := json.Unmarshal(data, &segments); err != nil {
		return nil, fmt.Errorf("parsing transcript: %w", err)
	}
	return segments, nil
}

// ToText converts segments to plain text with speaker labels.
func ToText(segments []Segment) string {
	var b strings.Builder
	lastSpeaker := ""
	for _, s := range segments {
		if s.Speaker != lastSpeaker {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(s.Speaker)
			b.WriteString(":\n")
			lastSpeaker = s.Speaker
		}
		b.WriteString(s.Content)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// ToSRT converts segments to SRT subtitle format.
func ToSRT(segments []Segment) string {
	var b strings.Builder
	for i, s := range segments {
		fmt.Fprintf(&b, "%d\n", i+1)
		fmt.Fprintf(&b, "%s --> %s\n", formatSRTTime(s.StartTime), formatSRTTime(s.EndTime))
		if s.Speaker != "" {
			fmt.Fprintf(&b, "[%s] %s\n\n", s.Speaker, s.Content)
		} else {
			fmt.Fprintf(&b, "%s\n\n", s.Content)
		}
	}
	return b.String()
}

// ToMarkdown converts segments to markdown with speaker labels and timestamps.
func ToMarkdown(segments []Segment) string {
	var b strings.Builder
	lastSpeaker := ""
	for _, s := range segments {
		if s.Speaker != lastSpeaker {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "**%s** (%s):\n", s.Speaker, formatTimestamp(s.StartTime))
			lastSpeaker = s.Speaker
		}
		b.WriteString(s.Content)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// Search performs a case-insensitive substring search across all segments.
func Search(segments []Segment, query string) []Match {
	lower := strings.ToLower(query)
	var matches []Match
	for i, s := range segments {
		if strings.Contains(strings.ToLower(s.Content), lower) {
			matches = append(matches, Match{Segment: s, Index: i})
		}
	}
	return matches
}

// Format converts parsed segments to the specified format (txt, srt, md).
// Returns the file extension and formatted content.
func Format(segments []Segment, format string) (string, string) {
	switch format {
	case "txt":
		return ".txt", ToText(segments)
	case "srt":
		return ".srt", ToSRT(segments)
	case "md":
		return ".md", ToMarkdown(segments)
	default:
		return ".txt", ToText(segments)
	}
}

// SanitizeFilename replaces characters that are invalid in filenames.
func SanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	s := replacer.Replace(name)
	s = strings.TrimSpace(s)
	if s == "" {
		s = "recording"
	}
	return s
}

// formatSRTTime formats milliseconds as HH:MM:SS,mmm for SRT format.
func formatSRTTime(ms int64) string {
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	ms %= 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// FormatTimestamp formats milliseconds as HH:MM:SS for display.
func FormatTimestamp(ms int64) string {
	return formatTimestamp(ms)
}

func formatTimestamp(ms int64) string {
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
