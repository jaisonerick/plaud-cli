package transcript

import (
	"strings"
	"testing"
)

const filed = `---
type: Transcript
title: "Semanal de 13/08"
---

**Jaison Erick (NexaEdge)** (00:00:37):
Fala, Pedro.

**SPEAKER_02** (00:00:39):
Opa, beleza?

**Aline Mazzoni (Mevo)** (00:04:17):
Não, não, não.

**SPEAKER_02** (00:05:41):
O lote fecha meia-noite, correto?
`

func TestReadTurnsTakesEveryTurnWithWhereItStarts(t *testing.T) {
	turns := ReadTurns(filed)

	if len(turns) != 4 {
		t.Fatalf("read %d turn(s), want 4", len(turns))
	}
	if turns[1].Speaker != "SPEAKER_02" || turns[1].StartMS != 39000 {
		t.Errorf("second turn is %q at %d ms", turns[1].Speaker, turns[1].StartMS)
	}
	if turns[2].Speaker != "Aline Mazzoni (Mevo)" {
		t.Errorf("a turn already carrying a person was not read: %q", turns[2].Speaker)
	}
}

func TestReadTurnsEndsEachTurnWhereTheNextBegins(t *testing.T) {
	turns := ReadTurns(filed)

	if turns[0].EndMS != turns[1].StartMS {
		t.Errorf("a turn ends at %d and the next starts at %d", turns[0].EndMS, turns[1].StartMS)
	}
	// The file says where the last turn starts and nothing about where it ends.
	if turns[3].EndMS != turns[3].StartMS+lastTurnMS {
		t.Errorf("the last turn runs to %d", turns[3].EndMS)
	}
}

func TestRewriteSpeakersLeavesEverythingItWasNotAskedAbout(t *testing.T) {
	turns := ReadTurns(filed)

	got, renamed := RewriteSpeakers(filed, map[int]string{turns[1].Line: "Paulo Ionescu (CERC)"})

	if renamed != 1 {
		t.Errorf("renamed %d turn(s), want 1", renamed)
	}
	if !strings.Contains(got, "**Paulo Ionescu (CERC)** (00:00:39):") {
		t.Errorf("the turn was not renamed:\n%s", got)
	}
	if !strings.Contains(got, `title: "Semanal de 13/08"`) {
		t.Error("the frontmatter the file gained after it was written was dropped")
	}
	if !strings.Contains(got, "**SPEAKER_02** (00:05:41):") {
		t.Error("a turn nobody asked about was renamed too")
	}
}

// A voice the file names wrongly is renamed like any other: what says who
// somebody is comes from the audio, and the name in the file is only a group.
func TestRewriteSpeakersCorrectsAPersonAlreadyWritten(t *testing.T) {
	turns := ReadTurns(filed)

	got, renamed := RewriteSpeakers(filed, map[int]string{turns[2].Line: "Amanda Destro (Aurora)"})

	if renamed != 1 {
		t.Fatalf("renamed %d turn(s), want 1", renamed)
	}
	if strings.Contains(got, "Aline Mazzoni") {
		t.Error("the wrong name survived")
	}
}

func TestRewriteSpeakersDoesNotTouchTheFileForANameItAlreadyHas(t *testing.T) {
	turns := ReadTurns(filed)

	got, renamed := RewriteSpeakers(filed, map[int]string{turns[0].Line: "Jaison Erick (NexaEdge)"})

	if renamed != 0 || got != filed {
		t.Errorf("the file was rewritten with nothing to change: %d turn(s)", renamed)
	}
}
