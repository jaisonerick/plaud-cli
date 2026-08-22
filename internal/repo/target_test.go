package repo

import (
	"path/filepath"
	"strings"
	"testing"
)

var saoPaulo = -3

// The recording every test here files: 2026-08-20 16:53 in São Paulo.
var recording = Recording{ID: "5899dc893bce51726285e516dadc1b4c", Name: "Reunião CERC / Vexia", Start: 1787255592448}

func TestATranscriptLandsWhereTheRepositoryScratchesByDefault(t *testing.T) {
	c := &Config{Root: "/repo", Scratch: "/repo/workspace/plaud", UTCOffset: &saoPaulo}

	got, err := c.Target(Profile{}, recording, ".md")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join("/repo/workspace/plaud", "2026-08-20-reunião-cerc-vexia-5899dc89.md")
	if got != want {
		t.Errorf("landed at %s, not %s", got, want)
	}
}

func TestACatalogFilesItsOwnTranscripts(t *testing.T) {
	c := &Config{Root: "/repo", Hub: "/repo/studio/plaud", UTCOffset: &saoPaulo}

	got, err := c.Target(Profile{}, recording, ".md")
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Dir(got) != "/repo/studio/plaud/transcripts" {
		t.Errorf("a catalog's transcript landed in %s", filepath.Dir(got))
	}
}

func TestAProfileOverridesWhereAndWhat(t *testing.T) {
	c := &Config{Root: "/repo", Scratch: "/repo/scratch", UTCOffset: &saoPaulo}
	p := Profile{Dest: "reunioes/{year}", Name: "{date}-{slug}.md"}

	got, err := c.Target(p, recording, ".md")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join("/repo", "reunioes/2026", "2026-08-20-reunião-cerc-vexia.md")
	if got != want {
		t.Errorf("landed at %s, not %s", got, want)
	}
}

func TestTheExtensionIsAddedOnlyWhenTheTemplateHasNone(t *testing.T) {
	c := &Config{Root: "/repo", Name: "{id}.markdown", UTCOffset: &saoPaulo}

	got, err := c.Target(Profile{}, recording, ".md")
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(got) != recording.ID+".markdown" {
		t.Errorf("the name came out as %s", filepath.Base(got))
	}
}

func TestAFieldNothingAnswersIsRefused(t *testing.T) {
	c := &Config{Root: "/repo", Name: "{date}-{project}.md"}

	_, err := c.Target(Profile{}, recording, ".md")
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Errorf("an unknown field produced %v", err)
	}
}

func TestTheDayIsTheRepositorysDayNotTheMachines(t *testing.T) {
	utc := 0
	c := &Config{Root: "/repo", Name: "{date}", UTCOffset: &utc}

	// 2026-08-20 16:53 in São Paulo is the 20th in UTC too; an hour before
	// midnight there is the next day in UTC, which is the case that matters.
	late := Recording{ID: "abc", Name: "x", Start: 1787272212448}
	got, err := c.Target(Profile{}, late, ".md")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "2026-08-21.md" {
		t.Errorf("filed under %s", filepath.Base(got))
	}

	c.UTCOffset = &saoPaulo
	got, err = c.Target(Profile{}, late, ".md")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "2026-08-20.md" {
		t.Errorf("São Paulo filed it under %s", filepath.Base(got))
	}
}

func TestARecordingWithNoTitleStillHasAName(t *testing.T) {
	c := &Config{Root: "/repo", UTCOffset: &saoPaulo}

	got, err := c.Target(Profile{}, Recording{ID: "abcdef1234", Name: "   ", Start: recording.Start}, ".md")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "2026-08-20-recording-abcdef12.md" {
		t.Errorf("the name came out as %s", filepath.Base(got))
	}
}
