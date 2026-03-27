package modal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	modalclient "github.com/modal-labs/modal-client/go"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

const (
	appName   = "modal-whisper"
	className = "WhisperTranscriber"
)

// Config holds the Modal authentication from environment variables.
type Config struct {
	TokenID     string
	TokenSecret string
}

// LoadConfig reads Modal credentials from environment variables, falling back
// to the saved config. Returns nil if neither source has credentials.
func LoadConfig(savedTokenID, savedTokenSecret string) *Config {
	tokenID := os.Getenv("MODAL_TOKEN_ID")
	tokenSecret := os.Getenv("MODAL_TOKEN_SECRET")

	if tokenID == "" {
		tokenID = savedTokenID
	}
	if tokenSecret == "" {
		tokenSecret = savedTokenSecret
	}

	if tokenID == "" || tokenSecret == "" {
		return nil
	}

	return &Config{
		TokenID:     tokenID,
		TokenSecret: tokenSecret,
	}
}

// TranscribeOpts holds optional parameters for the Whisper transcription.
type TranscribeOpts struct {
	Diarize            bool
	Polish             bool
	Compact            bool
	CompactGap         int    // Max silence gap in ms before starting a new paragraph
	Language           string // Force language code (e.g. "pt"), empty for auto-detect
	ContextDoc         string // Meeting context document contents
	SpeakerRecognition bool   // Enable matching against known speaker embeddings
}

// TranscribeResult holds the structured response from a Whisper transcription.
type TranscribeResult struct {
	AudioID  string              // Unique identifier for this transcription
	Segments []transcript.Segment // Transcript segments
	Speakers map[string]string   // SPEAKER_XX -> resolved name
}

// Transcribe sends audio data to a Modal-deployed Whisper function and returns
// a TranscribeResult with segments, audio ID, and speaker mapping.
func Transcribe(ctx context.Context, cfg *Config, audioData []byte, opts TranscribeOpts) (*TranscribeResult, error) {
	instance, err := getInstance(ctx, cfg)
	if err != nil {
		return nil, err
	}

	m, err := instance.Method("transcribe")
	if err != nil {
		return nil, fmt.Errorf("looking up method transcribe: %w", err)
	}

	kwargs := map[string]any{
		"diarize":             opts.Diarize,
		"polish":              opts.Polish,
		"compact":             opts.Compact,
		"compact_gap":         opts.CompactGap,
		"speaker_recognition": opts.SpeakerRecognition,
	}
	if opts.Language != "" {
		kwargs["language"] = opts.Language
	}
	if opts.ContextDoc != "" {
		kwargs["context_doc"] = opts.ContextDoc
	}

	result, err := m.Remote(ctx, []any{audioData}, kwargs)
	if err != nil {
		return nil, fmt.Errorf("calling modal function: %w", err)
	}

	// Modal's pickle-based protocol returns map[interface{}]interface{}.
	// Convert to JSON-compatible types before marshaling.
	converted := convertToJSON(result)

	raw, err := json.Marshal(converted)
	if err != nil {
		return nil, fmt.Errorf("marshaling modal result: %w", err)
	}

	// Parse the new dict response format
	var envelope struct {
		AudioID  string               `json:"audio_id"`
		Segments []transcript.Segment `json:"segments"`
		Speakers map[string]string    `json:"speakers"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parsing modal result: %w", err)
	}

	return &TranscribeResult{
		AudioID:  envelope.AudioID,
		Segments: envelope.Segments,
		Speakers: envelope.Speakers,
	}, nil
}

// getInstance creates a Modal client and returns a class instance for WhisperTranscriber.
func getInstance(ctx context.Context, cfg *Config) (*modalclient.ClsInstance, error) {
	client, err := modalclient.NewClientWithOptions(&modalclient.ClientParams{
		TokenID:     cfg.TokenID,
		TokenSecret: cfg.TokenSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("creating modal client: %w", err)
	}

	cls, err := client.Cls.FromName(ctx, appName, className, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up modal class %s/%s: %w", appName, className, err)
	}

	instance, err := cls.Instance(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("creating modal class instance: %w", err)
	}

	return instance, nil
}

// SetSpeakerName registers a speaker embedding under a name.
func SetSpeakerName(ctx context.Context, cfg *Config, audioID, speakerID, name string) error {
	instance, err := getInstance(ctx, cfg)
	if err != nil {
		return err
	}

	m, err := instance.Method("set_speaker_name")
	if err != nil {
		return fmt.Errorf("looking up method set_speaker_name: %w", err)
	}

	_, err = m.Remote(ctx, []any{audioID, speakerID, name}, nil)
	if err != nil {
		return fmt.Errorf("calling set_speaker_name: %w", err)
	}

	return nil
}

// ListKnownSpeakers returns the list of distinct known speaker names.
func ListKnownSpeakers(ctx context.Context, cfg *Config) ([]string, error) {
	instance, err := getInstance(ctx, cfg)
	if err != nil {
		return nil, err
	}

	m, err := instance.Method("list_known_speakers")
	if err != nil {
		return nil, fmt.Errorf("looking up method list_known_speakers: %w", err)
	}

	result, err := m.Remote(ctx, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("calling list_known_speakers: %w", err)
	}

	converted := convertToJSON(result)
	raw, err := json.Marshal(converted)
	if err != nil {
		return nil, fmt.Errorf("marshaling speaker list: %w", err)
	}

	var speakers []string
	if err := json.Unmarshal(raw, &speakers); err != nil {
		return nil, fmt.Errorf("parsing speaker list: %w", err)
	}

	return speakers, nil
}

// convertToJSON recursively converts map[interface{}]interface{} to map[string]any
// so the result can be marshaled to JSON.
func convertToJSON(v any) any {
	switch val := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(val))
		for k, v := range val {
			m[fmt.Sprintf("%v", k)] = convertToJSON(v)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(val))
		for k, v := range val {
			m[k] = convertToJSON(v)
		}
		return m
	case []any:
		s := make([]any, len(val))
		for i, v := range val {
			s[i] = convertToJSON(v)
		}
		return s
	default:
		return v
	}
}
