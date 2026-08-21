package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadContextTakesTheDescriptionItself(t *testing.T) {
	written := "Reunião da Vexia com a CERC sobre pagamentos. Éricles Bento, Zeni."

	got, err := readContext(written)
	if err != nil {
		t.Fatalf("readContext(%q) errored: %v", written, err)
	}
	if got != written {
		t.Errorf("readContext = %q, want the description back", got)
	}
}

func TestReadContextTakesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "briefing.md")
	if err := os.WriteFile(path, []byte("# Briefing\nCERC, Vexia."), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readContext(path)
	if err != nil {
		t.Fatalf("readContext errored: %v", err)
	}
	if !strings.Contains(got, "CERC, Vexia.") {
		t.Errorf("readContext = %q, want the file's contents", got)
	}
}

// A mistyped path read as prose polishes a transcript against the name of a
// file, which is worse than refusing: it looks like it worked.
func TestReadContextRefusesAPathThatIsNotThere(t *testing.T) {
	for _, value := range []string{
		"contexto/breifing.md",
		"briefing.md",
		"./nope.txt",
	} {
		if _, err := readContext(value); err == nil {
			t.Errorf("readContext(%q) accepted a missing file as prose", value)
		}
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
