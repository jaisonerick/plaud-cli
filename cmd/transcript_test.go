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
