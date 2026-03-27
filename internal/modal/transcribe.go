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
	method    = "transcribe"
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

// Transcribe sends audio data to a Modal-deployed Whisper function and returns
// transcript segments. The diarize flag requests speaker separation.
func Transcribe(ctx context.Context, cfg *Config, audioData []byte, diarize bool) ([]transcript.Segment, error) {
	client, err := modalclient.NewClientWithOptions(&modalclient.ClientParams{
		TokenID:     cfg.TokenID,
		TokenSecret: cfg.TokenSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("creating modal client: %w", err)
	}
	defer client.Close()

	cls, err := client.Cls.FromName(ctx, appName, className, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up modal class %s/%s: %w", appName, className, err)
	}

	instance, err := cls.Instance(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("creating modal class instance: %w", err)
	}

	m, err := instance.Method(method)
	if err != nil {
		return nil, fmt.Errorf("looking up method %s: %w", method, err)
	}

	result, err := m.Remote(ctx, []any{audioData}, map[string]any{
		"diarize": diarize,
	})
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

	var segments []transcript.Segment
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, fmt.Errorf("parsing modal result into segments: %w", err)
	}

	return segments, nil
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
