package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SpeakerSidecarSuffix is what marks the file carrying a transcript's voices.
const SpeakerSidecarSuffix = ".speakers.json"

// SpeakerFile travels beside a transcript so a voice can be named long after
// the recording was processed. It holds the embedding itself rather than a
// reference to one, which is what frees naming from anything the server kept.
type SpeakerFile struct {
	AudioID  string                  `json:"audio_id,omitempty"`
	Speakers map[string]SpeakerEntry `json:"speakers"`
}

// SpeakerEntry is one diarized voice: what it was called, and what it sounds like.
type SpeakerEntry struct {
	Name      string    `json:"name,omitempty"`
	Embedding []float64 `json:"embedding"`
}

// Labels returns the speaker labels in a stable order.
func (f *SpeakerFile) Labels() []string {
	labels := make([]string, 0, len(f.Speakers))
	for label := range f.Speakers {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// SpeakerSidecarPath is where the voices of the transcript at path belong.
func SpeakerSidecarPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + SpeakerSidecarSuffix
}

// WriteSpeakerFile saves the voices beside a transcript, and removes a stale
// sidecar when a run produced no embeddings at all.
func WriteSpeakerFile(path string, file SpeakerFile) error {
	if len(file.Speakers) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing stale speaker file: %w", err)
		}
		return nil
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling speaker file: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing speaker file: %w", err)
	}
	return nil
}

// ReadSpeakerFile loads the voices saved beside a transcript.
func ReadSpeakerFile(path string) (*SpeakerFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file SpeakerFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing speaker file: %w", err)
	}
	return &file, nil
}

// NewSpeakerFile pairs each diarized label with its embedding and resolved name.
func NewSpeakerFile(audioID string, names map[string]string, embeddings map[string][]float64) SpeakerFile {
	speakers := make(map[string]SpeakerEntry, len(embeddings))
	for label, embedding := range embeddings {
		entry := SpeakerEntry{Embedding: embedding}
		if name, ok := names[label]; ok && name != label {
			entry.Name = name
		}
		speakers[label] = entry
	}
	return SpeakerFile{AudioID: audioID, Speakers: speakers}
}
