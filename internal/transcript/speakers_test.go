package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpeakerSidecarSitsBesideTheTranscript(t *testing.T) {
	for _, tc := range []struct{ transcript, want string }{
		{"transcript.md", "transcript.speakers.json"},
		{"./out/meeting_whisper.srt", "./out/meeting_whisper.speakers.json"},
		{"/tmp/a_transcript.json", "/tmp/a_transcript.speakers.json"},
	} {
		if got := SpeakerSidecarPath(tc.transcript); got != tc.want {
			t.Errorf("SpeakerSidecarPath(%q) = %q, want %q", tc.transcript, got, tc.want)
		}
	}
}

func TestSpeakerFileSurvivesARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.speakers.json")
	written := NewSpeakerFile(
		"audio-1",
		map[string]string{"SPEAKER_00": "Jaison Erick", "SPEAKER_01": "SPEAKER_01"},
		map[string][]float64{"SPEAKER_00": {0.1, -0.2}, "SPEAKER_01": {0.3, 0.4}},
	)

	if err := WriteSpeakerFile(path, written); err != nil {
		t.Fatalf("WriteSpeakerFile: %v", err)
	}
	read, err := ReadSpeakerFile(path)
	if err != nil {
		t.Fatalf("ReadSpeakerFile: %v", err)
	}

	if read.AudioID != "audio-1" {
		t.Errorf("audio id = %q, want audio-1", read.AudioID)
	}
	if got := read.Speakers["SPEAKER_00"].Name; got != "Jaison Erick" {
		t.Errorf("SPEAKER_00 name = %q, want Jaison Erick", got)
	}
	if got := read.Speakers["SPEAKER_00"].Embedding; len(got) != 2 || got[1] != -0.2 {
		t.Errorf("SPEAKER_00 embedding = %v, want [0.1 -0.2]", got)
	}
}

func TestAnUnresolvedLabelIsNotItsOwnName(t *testing.T) {
	// The server reports an unmatched speaker as mapping to itself. Recording
	// that as a name would offer "SPEAKER_01" as a person to merge against.
	file := NewSpeakerFile("audio-1",
		map[string]string{"SPEAKER_01": "SPEAKER_01"},
		map[string][]float64{"SPEAKER_01": {0.3}},
	)
	if name := file.Speakers["SPEAKER_01"].Name; name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

func TestWritingNoSpeakersClearsAStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.speakers.json")
	if err := os.WriteFile(path, []byte(`{"speakers":{"SPEAKER_00":{"embedding":[1]}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	// A re-run without diarization must not leave the previous run's voices
	// beside a transcript they no longer describe.
	if err := WriteSpeakerFile(path, SpeakerFile{}); err != nil {
		t.Fatalf("WriteSpeakerFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stale speaker file survived, err = %v", err)
	}
}

func TestLabelsComeBackSorted(t *testing.T) {
	file := SpeakerFile{Speakers: map[string]SpeakerEntry{
		"SPEAKER_02": {}, "SPEAKER_00": {}, "SPEAKER_01": {},
	}}
	got := file.Labels()
	want := []string{"SPEAKER_00", "SPEAKER_01", "SPEAKER_02"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Labels() = %v, want %v", got, want)
		}
	}
}
