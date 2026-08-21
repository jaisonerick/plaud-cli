package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRanges(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ranges.json")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSpeakerRangesTakesTwoPeopleFromOneRecording(t *testing.T) {
	path := writeRanges(t, `[
	  {"name": "Jaison Erick", "company": "NexaEdge", "ranges": [[262000, 271000], [1044000, 1061000]]},
	  {"name": "Éricles Bento", "company": "CERC", "ranges": [[279000, 304000]]}
	]`)

	specs, err := readSpeakerRanges(path)
	if err != nil {
		t.Fatalf("readSpeakerRanges errored: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("read %d people, want 2", len(specs))
	}
	if specs[0].Name != "Jaison Erick" || specs[0].Company != "NexaEdge" {
		t.Errorf("first person is %q at %q", specs[0].Name, specs[0].Company)
	}
	if got := speechIn(specs[0]); got != 26000 {
		t.Errorf("speechIn = %d ms, want 26000", got)
	}
}

func TestReadSpeakerRangesRefusesALoneFirstName(t *testing.T) {
	path := writeRanges(t, `[{"name": "Amanda", "company": "Aurora", "ranges": [[1000, 9000]]}]`)

	_, err := readSpeakerRanges(path)
	if err == nil {
		t.Fatal("a lone first name was accepted")
	}
	if !strings.Contains(err.Error(), "surname_unknown") {
		t.Errorf("error does not say how to record an unknown surname: %v", err)
	}
}

func TestReadSpeakerRangesTakesALoneFirstNameWhenSaidToBeUnknown(t *testing.T) {
	path := writeRanges(t, `[{"name": "Adriele", "company": "Vexia", "surname_unknown": true, "ranges": [[1000, 9000]]}]`)

	specs, err := readSpeakerRanges(path)
	if err != nil {
		t.Fatalf("readSpeakerRanges errored: %v", err)
	}
	if specs[0].Name != "Adriele" {
		t.Errorf("name is %q", specs[0].Name)
	}
}

func TestReadSpeakerRangesRefusesARangeThatEndsBeforeItStarts(t *testing.T) {
	path := writeRanges(t, `[{"name": "Jaison Erick", "company": "NexaEdge", "ranges": [[271000, 262000]]}]`)

	_, err := readSpeakerRanges(path)
	if err == nil {
		t.Fatal("a backwards range was accepted")
	}
}

func TestReadSpeakerRangesRefusesAPersonWithNoRanges(t *testing.T) {
	path := writeRanges(t, `[{"name": "Jaison Erick", "company": "NexaEdge", "ranges": []}]`)

	if _, err := readSpeakerRanges(path); err == nil {
		t.Fatal("a person with nothing to listen to was accepted")
	}
}

func TestReadSpeakerRangesRefusesAMissingCompany(t *testing.T) {
	path := writeRanges(t, `[{"name": "Jaison Erick", "ranges": [[1000, 9000]]}]`)

	if _, err := readSpeakerRanges(path); err == nil {
		t.Fatal("a person with no company was accepted")
	}
}

func TestDurationReadsAsMinutesPastAMinute(t *testing.T) {
	for ms, want := range map[int]string{9000: "9s", 26000: "26s", 61000: "1m01s", 605000: "10m05s"} {
		if got := duration(ms); got != want {
			t.Errorf("duration(%d) = %q, want %q", ms, got, want)
		}
	}
}
