package modal

import (
	"encoding/json"
	"strings"
	"testing"
)

// The wire shape is the contract with services/whisper/modal_whisper/builder.py.
const resultEvent = `{
  "type": "result",
  "audio_id": "e348561a6b26d65c939add422cf48341",
  "segments": [{"start_time": 0, "end_time": 900, "content": "Bom dia", "speaker": "SPEAKER_00"}],
  "speakers": {"SPEAKER_00": "Jaison Erick", "SPEAKER_01": "SPEAKER_01"}
}`

func TestResultEventCarriesEveryFieldThrough(t *testing.T) {
	var event SSEEvent
	if err := json.Unmarshal([]byte(resultEvent), &event); err != nil {
		t.Fatalf("unmarshalling the result event: %v", err)
	}

	result := event.Result()

	if result.AudioID != "e348561a6b26d65c939add422cf48341" {
		t.Errorf("audio id = %q, want the recording's", result.AudioID)
	}
	if len(result.Segments) != 1 || result.Segments[0].Content != "Bom dia" {
		t.Errorf("segments = %+v, want one saying \"Bom dia\"", result.Segments)
	}
	if result.Speakers["SPEAKER_00"] != "Jaison Erick" {
		t.Errorf("speakers = %v, want SPEAKER_00 named Jaison Erick", result.Speakers)
	}
}

func TestNoVoiceEverReachesTheCaller(t *testing.T) {
	// The store is shared, so an embedding that travelled would put people's
	// voices on the laptop of everyone who happens to transcribe a meeting
	// they were in. Nothing here should have anywhere to put one.
	var event SSEEvent
	if err := json.Unmarshal([]byte(`{
	  "type": "result",
	  "audio_id": "x",
	  "speakers": {"SPEAKER_00": "SPEAKER_00"},
	  "embeddings": {"SPEAKER_00": [0.1, -0.2]}
	}`), &event); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(event.Result())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "0.1") || strings.Contains(strings.ToLower(string(encoded)), "embedding") {
		t.Errorf("the result kept a voice: %s", encoded)
	}
}

func TestTranscribeOptionsNameTheRecording(t *testing.T) {
	// The service keys a transcription's voices by this, and naming a speaker
	// afterwards is only possible because it does.
	encoded, err := json.Marshal(TranscribeOpts{RecordingID: "abc123", ContextDoc: "an agenda"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"recording_id":"abc123"`) {
		t.Errorf("options = %s, want the recording id in them", encoded)
	}
}
