package transcript

import (
	"strings"
	"testing"
)

func TestFieldsAreAddedToATranscriptThatHasNoFrontMatter(t *testing.T) {
	got := WriteFields(plain, []Field{{RecordingKey, "5899dc89"}, {"type", "Transcript"}})

	if ReadField(got, RecordingKey) != "5899dc89" {
		t.Errorf("the recording did not come back:\n%s", got)
	}
	if !strings.Contains(got, "**Jaison Erick (NexaEdge)** (00:00:37):") {
		t.Errorf("the transcript itself was disturbed:\n%s", got)
	}
	if strings.Count(got, "---\n") != 2 {
		t.Errorf("the file gained more than one front matter:\n%s", got)
	}
}

func TestTheVoicesBlockStaysLastAndIntact(t *testing.T) {
	withVoices := WriteVoices(plain, VoiceBlock{"SPEAKER_02": {"v_91bc04"}})

	got := WriteFields(withVoices, []Field{{RecordingKey, "abc123"}})

	if ReadVoices(got)["SPEAKER_02"][0] != "v_91bc04" {
		t.Errorf("the voices were lost:\n%s", got)
	}
	if strings.Index(got, "voices:") < strings.Index(got, RecordingKey+":") {
		t.Errorf("the voices block stopped being last:\n%s", got)
	}
}

func TestWritingTheSameFieldAgainReplacesIt(t *testing.T) {
	once := WriteFields(plain, []Field{{"type", "Draft"}})

	twice := WriteFields(once, []Field{{"type", "Transcript"}})

	if strings.Count(twice, "type:") != 1 {
		t.Errorf("the key was written twice:\n%s", twice)
	}
	if ReadField(twice, "type") != "Transcript" {
		t.Errorf("the value did not take:\n%s", twice)
	}
}

func TestWhatTheFrontMatterAlreadySaidSurvives(t *testing.T) {
	filed := "---\ntitle: \"Semanal\"\ntags: [dinie, rtb]\n---\n\n" + plain

	got := WriteFields(filed, []Field{{RecordingKey, "abc123"}})

	if !strings.Contains(got, `title: "Semanal"`) || !strings.Contains(got, "tags: [dinie, rtb]") {
		t.Errorf("an edit made after filing was dropped:\n%s", got)
	}
}

func TestAValueYAMLWouldReadAsSomethingElseIsQuoted(t *testing.T) {
	got := WriteFields(plain, []Field{{"title", "2026-08-20 16:53:12"}})

	if ReadField(got, "title") != "2026-08-20 16:53:12" {
		t.Errorf("a title carrying colons came back as %q", ReadField(got, "title"))
	}
	if !strings.Contains(got, `title: "2026-08-20 16:53:12"`) {
		t.Errorf("the value was left bare:\n%s", got)
	}
}

func TestATranscriptWithNoFieldsIsUntouched(t *testing.T) {
	if got := WriteFields(plain, nil); got != plain {
		t.Errorf("a transcript with nothing to write was rewritten:\n%s", got)
	}
}
