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

func TestDropTurnsTakesTheTurnAndTheBlankLineClosingIt(t *testing.T) {
	turns := ReadTurns(filed)

	got, dropped := DropTurns(filed, map[int]bool{turns[1].Line: true})

	if dropped != 1 {
		t.Fatalf("dropped %d turn(s), want 1", dropped)
	}
	if strings.Contains(got, "Opa, beleza?") {
		t.Error("the speech of the dropped turn survived")
	}
	if strings.Contains(got, "(00:00:39)") {
		t.Error("the header of the dropped turn survived")
	}
	if !strings.Contains(got, "**Jaison Erick (NexaEdge)** (00:00:37):\nFala, Pedro.\n\n**Aline") {
		t.Errorf("the turns around it did not close up:\n%s", got)
	}
}

func TestDropTurnsLeavesTheRestOfTheFileAsItWas(t *testing.T) {
	turns := ReadTurns(filed)

	got, _ := DropTurns(filed, map[int]bool{turns[3].Line: true})

	if !strings.Contains(got, `title: "Semanal de 13/08"`) {
		t.Error("the front matter was dropped with the turn")
	}
	if !strings.Contains(got, "Não, não, não.") {
		t.Error("a turn nobody asked about went with it")
	}
}

// A transcript gains headings and notes after it is filed, and they sit
// between the turns: a turn ends at the blank line after its speech, never at
// whatever comes next.
func TestDropTurnsKeepsWhatSomebodyWroteAfterTheLastTurn(t *testing.T) {
	annotated := filed + "\n## Combinados\n\nFechar o lote hoje.\n"
	turns := ReadTurns(annotated)

	got, dropped := DropTurns(annotated, map[int]bool{turns[3].Line: true})

	if dropped != 1 {
		t.Fatalf("dropped %d turn(s), want 1", dropped)
	}
	if !strings.Contains(got, "## Combinados") || !strings.Contains(got, "Fechar o lote hoje.") {
		t.Errorf("the section after the turn went with it:\n%s", got)
	}
}

func TestDropTurnsIgnoresALineThatIsNotATurn(t *testing.T) {
	got, dropped := DropTurns(filed, map[int]bool{1: true})

	if dropped != 0 || got != filed {
		t.Errorf("dropped %d line(s) that hold no turn", dropped)
	}
}

func TestSpeechOfIsTheLinesUnderTheHeader(t *testing.T) {
	lines := strings.Split(filed, "\n")
	turns := ReadTurns(filed)

	spoken := SpeechOf(lines, turns[2].Line)

	if len(spoken) != 1 || spoken[0] != "Não, não, não." {
		t.Errorf("read %q as the speech of a turn", spoken)
	}
}
