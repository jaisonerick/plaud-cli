package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

func TestContextTakesTheDescriptionItself(t *testing.T) {
	written := "Reunião da Vexia com a CERC em 12/08/2026. Éricles Bento, Zeni."

	got, err := readContext(written, "")
	if err != nil {
		t.Fatalf("readContext errored: %v", err)
	}
	if got != written {
		t.Errorf("readContext = %q, want the description back", got)
	}
}

func TestContextFileIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "briefing.md")
	if err := os.WriteFile(path, []byte("# Briefing\nCERC, Vexia."), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readContext("", path)
	if err != nil {
		t.Fatalf("readContext errored: %v", err)
	}
	if !strings.Contains(got, "CERC, Vexia.") {
		t.Errorf("the file was not read: %q", got)
	}
}

func TestContextFileThatIsNotThereIsRefused(t *testing.T) {
	_, err := readContext("", filepath.Join(t.TempDir(), "gone.md"))
	if err == nil {
		t.Fatal("a missing context file was accepted")
	}
}

// The description is text even when it reads like a path, which is what the
// guessing got wrong: a date in Portuguese carries a slash.
func TestADescriptionWithASlashIsStillTheDescription(t *testing.T) {
	written := "Semanal de 12/08/2026 com a Vexia"

	got, err := readContext(written, "")
	if err != nil || got != written {
		t.Errorf("got %q, err %v", got, err)
	}
}

func TestAFileNamedAsTheDescriptionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "briefing.md")
	if err := os.WriteFile(path, []byte("CERC"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := readContext(path, "")
	if err == nil {
		t.Fatal("a file passed as the description was read as prose")
	}
	if !strings.Contains(err.Error(), "--context-file") {
		t.Errorf("the error does not say which flag reads a file: %v", err)
	}
}

func TestTheTwoContextFlagsAreNotBothTaken(t *testing.T) {
	if _, err := readContext("uma descrição", "/tmp/x.md"); err == nil {
		t.Fatal("both flags were accepted at once")
	}
}

func TestAgreedNameTakesWhatEveryVoiceOfTheNameSays(t *testing.T) {
	names := map[string]string{"v_1": "Amanda Destro (Aurora)", "v_2": "Amanda Destro (Aurora)"}

	got, split := agreedName([]string{"v_1", "v_2"}, names)

	if split {
		t.Fatal("two voices of one person were read as a disagreement")
	}
	if got != "Amanda Destro (Aurora)" {
		t.Errorf("agreed on %q", got)
	}
}

func TestAgreedNameReportsVoicesThatDisagree(t *testing.T) {
	names := map[string]string{"v_1": "Amanda Destro (Aurora)", "v_2": "Matheus Zeni (CERC)"}

	got, split := agreedName([]string{"v_1", "v_2"}, names)

	if !split {
		t.Fatal("two people under one name were merged into one answer")
	}
	if got != "" {
		t.Errorf("a name came back anyway: %q", got)
	}
}

func TestAgreedNameIgnoresAVoiceNobodyCanPlace(t *testing.T) {
	names := map[string]string{"v_1": "", "v_2": "Amanda Destro (Aurora)"}

	got, split := agreedName([]string{"v_1", "v_2"}, names)

	if split || got != "Amanda Destro (Aurora)" {
		t.Errorf("got %q, split %v", got, split)
	}
}

func TestRuledOutTakesANameWhoseEveryVoiceIsOutOfTheMeeting(t *testing.T) {
	voices := transcript.VoiceBlock{
		"SPEAKER_00": {"v_aaa"},
		"SPEAKER_01": {"v_bbb", "v_ccc"},
	}

	out, disputed := ruledOut(voices, map[string]bool{"v_bbb": true, "v_ccc": true})

	if !out["SPEAKER_01"] || out["SPEAKER_00"] {
		t.Errorf("ruled out %v", out)
	}
	if len(disputed) != 0 {
		t.Errorf("a name every voice of which is out was reported as disputed: %v", disputed)
	}
}

// A turn says only the name, so there is no way to drop half of what is
// written under one.
func TestRuledOutLeavesANameWhoseVoicesDisagree(t *testing.T) {
	voices := transcript.VoiceBlock{"SPEAKER_01": {"v_bbb", "v_ccc"}}

	out, disputed := ruledOut(voices, map[string]bool{"v_bbb": true})

	if len(out) != 0 {
		t.Errorf("turns were dropped on half a name: %v", out)
	}
	if len(disputed) != 1 || disputed[0] != "SPEAKER_01" {
		t.Errorf("the name nobody can settle was reported as %v", disputed)
	}
}

func TestRuledOutTakesNothingWhenNobodyWasRuledOut(t *testing.T) {
	voices := transcript.VoiceBlock{"Jaison Erick (NexaEdge)": {"v_aaa"}}

	out, disputed := ruledOut(voices, map[string]bool{"v_aaa": false})

	if len(out) != 0 || len(disputed) != 0 {
		t.Errorf("ruled out %v, disputed %v", out, disputed)
	}
}
