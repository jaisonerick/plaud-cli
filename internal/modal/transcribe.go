package modal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	modalclient "github.com/modal-labs/modal-client/go"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

// Config holds the Modal connection settings from environment variables.
type Config struct {
	TokenID      string
	TokenSecret  string
	AppName      string
	FunctionName string
}

// LoadConfig reads Modal configuration from environment variables.
// Returns nil if the required variables are not set.
func LoadConfig() *Config {
	tokenID := os.Getenv("MODAL_TOKEN_ID")
	tokenSecret := os.Getenv("MODAL_TOKEN_SECRET")
	appName := os.Getenv("MODAL_APP_NAME")
	functionName := os.Getenv("MODAL_FUNCTION_NAME")

	if tokenID == "" || tokenSecret == "" || appName == "" || functionName == "" {
		return nil
	}

	return &Config{
		TokenID:      tokenID,
		TokenSecret:  tokenSecret,
		AppName:      appName,
		FunctionName: functionName,
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

	// FunctionName can be "ClassName.method" for class methods or just "function_name"
	var result any
	if parts := strings.SplitN(cfg.FunctionName, ".", 2); len(parts) == 2 {
		cls, err := client.Cls.FromName(ctx, cfg.AppName, parts[0], nil)
		if err != nil {
			return nil, fmt.Errorf("looking up modal class %s/%s: %w", cfg.AppName, parts[0], err)
		}

		instance, err := cls.Instance(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("creating modal class instance: %w", err)
		}

		method, err := instance.Method(parts[1])
		if err != nil {
			return nil, fmt.Errorf("looking up method %s: %w", parts[1], err)
		}

		result, err = method.Remote(ctx, []any{audioData}, map[string]any{
			"diarize": diarize,
		})
		if err != nil {
			return nil, fmt.Errorf("calling modal method: %w", err)
		}
	} else {
		fn, err := client.Functions.FromName(ctx, cfg.AppName, cfg.FunctionName, nil)
		if err != nil {
			return nil, fmt.Errorf("looking up modal function %s/%s: %w", cfg.AppName, cfg.FunctionName, err)
		}

		result, err = fn.Remote(ctx, []any{audioData}, map[string]any{
			"diarize": diarize,
		})
		if err != nil {
			return nil, fmt.Errorf("calling modal function: %w", err)
		}
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
