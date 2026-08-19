package modal

import (
	"encoding/json"
	"testing"
)

// The wire shape is the contract with services/whisper/modal_whisper/builder.py.
const resultEvent = `{
  "type": "result",
  "audio_id": "abc-123",
  "segments": [{"start_time": 0, "end_time": 900, "content": "Bom dia", "speaker": "SPEAKER_00"}],
  "speakers": {"SPEAKER_00": "Jaison Erick", "SPEAKER_01": "SPEAKER_01"},
  "embeddings": {"SPEAKER_00": [0.1, -0.2], "SPEAKER_01": [0.3, 0.4]}
}`

func TestResultEventCarriesEveryFieldThrough(t *testing.T) {
	var event SSEEvent
	if err := json.Unmarshal([]byte(resultEvent), &event); err != nil {
		t.Fatalf("unmarshalling the result event: %v", err)
	}

	result := event.Result()

	if result.AudioID != "abc-123" {
		t.Errorf("audio id = %q, want abc-123", result.AudioID)
	}
	if len(result.Segments) != 1 || result.Segments[0].Content != "Bom dia" {
		t.Errorf("segments = %+v, want one saying \"Bom dia\"", result.Segments)
	}
	if result.Speakers["SPEAKER_00"] != "Jaison Erick" {
		t.Errorf("speakers = %v, want SPEAKER_00 named Jaison Erick", result.Speakers)
	}

	// The embeddings are the whole reason a speaker can be named from a saved
	// transcript. Dropping them here costs nothing visible until someone tries.
	embedding, ok := result.Embeddings["SPEAKER_00"]
	if !ok {
		t.Fatalf("embeddings = %v, want one for SPEAKER_00", result.Embeddings)
	}
	if len(embedding) != 2 || embedding[1] != -0.2 {
		t.Errorf("SPEAKER_00 embedding = %v, want [0.1 -0.2]", embedding)
	}
}

func TestResultEventWithoutDiarizationHasNoVoices(t *testing.T) {
	var event SSEEvent
	if err := json.Unmarshal([]byte(`{"type":"result","audio_id":"x","segments":[],"speakers":{},"embeddings":{}}`), &event); err != nil {
		t.Fatal(err)
	}
	if got := event.Result().Embeddings; len(got) != 0 {
		t.Errorf("embeddings = %v, want none", got)
	}
}
