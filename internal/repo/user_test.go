package repo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// alone points the person's settings at a file of this test's own, so a test
// never reads whatever the machine running it happens to have.
func alone(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if content != "" {
		write(t, path, content)
	}
	t.Setenv("PLAUD_SETTINGS", path)
	return path
}

func TestWhatAPersonSetsAboutARepositoryWins(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"scratch": "workspace/plaud", "language": "pt"}`)
	alone(t, `{"repositories": {"`+root+`": {"scratch": "meu/lugar"}}}`)

	c, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}

	if c.Scratch != filepath.Join(root, "meu/lugar") {
		t.Errorf("scratch came back as %s", c.Scratch)
	}
	if c.Where("scratch") != "your settings for this repository" {
		t.Errorf("scratch was credited to %q", c.Where("scratch"))
	}
	if c.Language != "pt" {
		t.Errorf("a key the person did not set was cleared: language is %q", c.Language)
	}
}

func TestTheRepositoryWinsOverWhatAPersonCarriesEverywhere(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"scratch": "workspace/plaud"}`)
	alone(t, `{"defaults": {"scratch": "sempre-aqui", "utc_offset": -3}}`)

	c, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}

	if c.Scratch != filepath.Join(root, "workspace/plaud") {
		t.Errorf("a default overrode what the repository declared: %s", c.Scratch)
	}
	if c.UTCOffset == nil || *c.UTCOffset != -3 {
		t.Errorf("a default the repository says nothing about did not apply: %v", c.UTCOffset)
	}
	if c.Where("utc_offset") != "your defaults" {
		t.Errorf("the offset was credited to %q", c.Where("utc_offset"))
	}
}

func TestTheTwoHalvesOfAProfileMeet(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"profiles": {"cerc": {"dest": "reunioes/{year}", "front_matter": {"client": "CERC"}}}}`)
	alone(t, `{"repositories": {"`+root+`": {"profiles": {"cerc": {"tag": "PPFX - Amanda"}}}}}`)

	c, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}

	profile, ok := c.Profile("cerc")
	if !ok {
		t.Fatal("the profile is gone")
	}
	if profile.Tag != "PPFX - Amanda" {
		t.Errorf("the person's half is missing: tag is %q", profile.Tag)
	}
	if profile.Dest != "reunioes/{year}" || profile.FrontMatter["client"] != "CERC" {
		t.Errorf("the repository's half was replaced: %+v", profile)
	}
}

func TestAProfileCanBeAPersonsEntirely(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"scratch": "workspace"}`)
	alone(t, `{"repositories": {"`+root+`": {"profiles": {"meu": {"tag": "Dinie"}}}}}`)

	c, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}

	if profile, ok := c.Profile("meu"); !ok || profile.Tag != "Dinie" {
		t.Errorf("a profile the repository never declared came back as %+v (%v)", profile, ok)
	}
}

func TestARepositoryIsNamedByWhereItIsHosted(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@github.com:PPFX-Labs/cerc-transformation.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git is not usable here: %s", out)
		}
	}

	if got := Identity(root); got != "github.com/PPFX-Labs/cerc-transformation" {
		t.Errorf("identity came back as %q", got)
	}
}

func TestARepositoryWithNoRemoteIsNamedByItsPath(t *testing.T) {
	root := t.TempDir()

	if got := Identity(root); got != root {
		t.Errorf("identity came back as %q, not the path", got)
	}
}

func TestAnEmptySettingsFileIsNoSettings(t *testing.T) {
	alone(t, "\n")

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("an empty file failed with %v", err)
	}
	if len(settings.Repositories) != 0 || settings.Defaults != nil {
		t.Errorf("an empty file produced %+v", settings)
	}
}
