package speaker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIgnoresHowTheSpellingWasCased(t *testing.T) {
	path := filepath.Join(t.TempDir(), AliasesFile)
	write(t, path, map[string]string{"luca": "Luca Bianchi", "Vic": "Victoria Dinie"})

	aliases, err := LoadAliases(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"luca", "Luca", "LUCA"} {
		if got := aliases.Resolve(spelling); got != "Luca Bianchi" {
			t.Errorf("Resolve(%q) = %q, want Luca Bianchi", spelling, got)
		}
	}
	if got := aliases.Resolve("Zeni"); got != "Zeni" {
		t.Errorf("Resolve(Zeni) = %q, want it unchanged", got)
	}
}

func TestAnEmptyAnswerLeavesTheSpellingAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), AliasesFile)
	write(t, path, map[string]string{"Tom": ""})

	aliases, err := LoadAliases(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := aliases.Resolve("Tom"); got != "Tom" {
		t.Errorf("Resolve(Tom) = %q, want it unchanged", got)
	}
}

func TestAMissingFileIsNotAnError(t *testing.T) {
	aliases, err := LoadAliases(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadAliases on a missing file: %v", err)
	}
	if got := aliases.Resolve("Tom"); got != "Tom" {
		t.Errorf("Resolve(Tom) = %q, want it unchanged", got)
	}
}

func TestWritingTheTemplateKeepsTheAnswersAlreadyGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), AliasesFile)
	write(t, path, map[string]string{"luca": "Luca Bianchi"})

	blank, err := WriteTemplate(path, []string{"luca", "Vic", "Tom"})
	if err != nil {
		t.Fatal(err)
	}
	if blank != 2 {
		t.Errorf("blank = %d, want 2", blank)
	}

	var entries map[string]string
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("the template it wrote does not parse: %v", err)
	}
	if entries["luca"] != "Luca Bianchi" {
		t.Errorf("luca = %q, want the answer kept", entries["luca"])
	}
	if _, ok := entries["Vic"]; !ok {
		t.Error("Vic is missing from the template")
	}
}

func write(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
