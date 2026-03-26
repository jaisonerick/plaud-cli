package modal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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

	fn, err := client.Functions.FromName(ctx, cfg.AppName, cfg.FunctionName, nil)
	if err != nil {
		return nil, fmt.Errorf("looking up modal function %s/%s: %w", cfg.AppName, cfg.FunctionName, err)
	}

	result, err := fn.Remote(ctx, []any{audioData}, map[string]any{
		"diarize": diarize,
	})
	if err != nil {
		return nil, fmt.Errorf("calling modal function: %w", err)
	}

	// The Modal function returns a JSON-serializable structure.
	// Marshal back to JSON then unmarshal into our Segment type.
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshaling modal result: %w", err)
	}

	var segments []transcript.Segment
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, fmt.Errorf("parsing modal result into segments: %w", err)
	}

	return segments, nil
}
