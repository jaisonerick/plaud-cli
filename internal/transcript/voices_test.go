package transcript

import (
	"strings"
	"testing"
)

const plain = "**Jaison Erick (NexaEdge)** (00:00:37):\nFala, Pedro.\n"

func TestVoicesSurviveWritingAndReadingBack(t *testing.T) {
	written := WriteVoices(plain, VoiceBlock{
		"Jaison Erick (NexaEdge)": {"v_7f3a91"},
		"SPEAKER_02":              {"v_91bc04", "v_0d2e11"},
	})

	got := ReadVoices(written)

	if len(got) != 2 {
		t.Fatalf("read %d name(s) back: %v", len(got), got)
	}
	if len(got["SPEAKER_02"]) != 2 || got["SPEAKER_02"][0] != "v_91bc04" {
		t.Errorf("a name holding two voices came back as %v", got["SPEAKER_02"])
	}
	if !strings.Contains(written, "**Jaison Erick (NexaEdge)** (00:00:37):") {
		t.Error("the transcript itself was disturbed")
	}
}

func TestVoicesGoUnderFrontMatterAlreadyThere(t *testing.T) {
	filed := "---\ntype: Transcript\ntitle: \"Semanal\"\n---\n\n" + plain

	written := WriteVoices(filed, VoiceBlock{"SPEAKER_02": {"v_91bc04"}})

	if strings.Count(written, "---\n") != 2 {
		t.Errorf("the file ended up with more than one front matter:\n%s", written)
	}
	if !strings.Contains(written, "type: Transcript") || !strings.Contains(written, `title: "Semanal"`) {
		t.Errorf("what the front matter already said was lost:\n%s", written)
	}
	if ReadVoices(written)["SPEAKER_02"][0] != "v_91bc04" {
		t.Error("the voices were not read back")
	}
}

func TestWritingVoicesAgainReplacesTheBlock(t *testing.T) {
	once := WriteVoices(plain, VoiceBlock{"SPEAKER_02": {"v_91bc04"}})
	twice := WriteVoices(once, VoiceBlock{"Paulo Ionescu (CERC)": {"v_91bc04"}})

	if strings.Contains(twice, "SPEAKER_02") {
		t.Errorf("the old block was kept alongside the new one:\n%s", twice)
	}
	if ReadVoices(twice)["Paulo Ionescu (CERC)"][0] != "v_91bc04" {
		t.Error("the new block did not take")
	}
}

func TestATranscriptWrittenBeforeTheIdsHasNoBlock(t *testing.T) {
	if got := ReadVoices(plain); got != nil {
		t.Errorf("a file with no block answered %v", got)
	}
	if got := ReadVoices("---\ntype: Transcript\n---\n\n" + plain); got != nil {
		t.Errorf("front matter without voices answered %v", got)
	}
}

func TestRenameCarriesEveryVoiceOfTheName(t *testing.T) {
	voices := VoiceBlock{"SPEAKER_02": {"v_91bc04", "v_0d2e11"}}

	voices.Rename("SPEAKER_02", "Paulo Ionescu (CERC)")

	if _, still := voices["SPEAKER_02"]; still {
		t.Error("the old name stayed behind")
	}
	if len(voices["Paulo Ionescu (CERC)"]) != 2 {
		t.Errorf("the new name holds %v", voices["Paulo Ionescu (CERC)"])
	}
}
