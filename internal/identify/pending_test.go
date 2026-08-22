package identify

import (
	"os"
	"path/filepath"
	"testing"
)

const filed = `---
recording: bf1ee96b1f14cff5c2d71bf6fda842f0
voices:
  "Jaison Erick (NexaEdge)": [v_e9e166cd8850]
  "SPEAKER_02": [v_91bc04aa1122]
  "SPEAKER_05": [v_0d2e11ff3344]
---

**Jaison Erick (NexaEdge)** (00:00:01):
Boa, vamos começar.

**SPEAKER_02** (00:00:09):
Uma frase curta.

**SPEAKER_02** (00:01:30):
Uma resposta bem mais longa, que é a que serve de amostra porque tem fala suficiente para reconhecer alguém.

**SPEAKER_05** (00:04:00):
Só isso.
`

func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAVoiceWithNoNameIsFoundByItsId(t *testing.T) {
	root := t.TempDir()
	put(t, root, "transcricoes/reuniao.md", filed)

	voices, _, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(voices) != 2 {
		t.Fatalf("found %d voice(s): %+v", len(voices), voices)
	}
	if voices[0].ID != "v_91bc04aa1122" || voices[0].Label != "SPEAKER_02" {
		t.Errorf("the first came back as %+v", voices[0])
	}
	if voices[0].Recording != "bf1ee96b1f14cff5c2d71bf6fda842f0" {
		t.Errorf("the recording came back as %q", voices[0].Recording)
	}
}

func TestASampleIsTheLongestStretchThatVoiceHolds(t *testing.T) {
	root := t.TempDir()
	put(t, root, "reuniao.md", filed)

	voices, _, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}

	samples := voices[0].Samples
	if len(samples) != 2 {
		t.Fatalf("SPEAKER_02 came back with %d sample(s)", len(samples))
	}
	if samples[0].StartSec != 9 || samples[1].StartSec != 90 {
		t.Errorf("the samples start at %v and %v", samples[0].StartSec, samples[1].StartSec)
	}
	if samples[1].Text == "" {
		t.Error("a sample carries no text, so nothing on the page says what was said")
	}
}

func TestANamedTranscriptHasNothingPending(t *testing.T) {
	root := t.TempDir()
	put(t, root, "pronta.md", `---
recording: abc123
voices:
  "Jaison Erick (NexaEdge)": [v_1]
---

**Jaison Erick (NexaEdge)** (00:00:01):
Só eu falando.
`)

	voices, _, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 0 {
		t.Errorf("a finished transcript offered %+v", voices)
	}
}

func TestAFileThatIsNotATranscriptIsIgnored(t *testing.T) {
	root := t.TempDir()
	put(t, root, "notas.md", "# Notas\n\n**SPEAKER_02** (00:00:09):\nnão é transcrição\n")
	put(t, root, ".git/objects/x.md", filed)
	put(t, root, "node_modules/pacote/leia.md", filed)

	voices, _, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 0 {
		t.Errorf("something outside a transcript was offered: %+v", voices)
	}
}

func TestATranscriptWrittenBeforeTheIdsAsksByItsLabels(t *testing.T) {
	root := t.TempDir()
	put(t, root, "antiga.md", `---
recording: abc123
---

**SPEAKER_00** (00:00:01):
Escrita antes das vozes terem id.

**SPEAKER_00** (00:02:00):
Uma fala mais longa, para virar a amostra.
`)

	voices, _, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 1 || voices[0].ID != "SPEAKER_00" {
		t.Errorf("an older transcript produced %+v", voices)
	}
}

func TestATranscriptThatNeverSaidWhichRecordingIsReportedNotSkipped(t *testing.T) {
	root := t.TempDir()
	put(t, root, "antiga-sem-id.md", `---
title: "Semanal"
---

**SPEAKER_00** (00:00:01):
Escrita quando o arquivo não guardava a gravação.
`)

	voices, unreachable, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 0 {
		t.Errorf("a file nothing can be asked about was offered: %+v", voices)
	}
	if len(unreachable) != 1 {
		t.Fatalf("it was skipped in silence: %v", unreachable)
	}
}

func TestProseIsNotReportedAsUnreachable(t *testing.T) {
	root := t.TempDir()
	put(t, root, "leia.md", "# Notas\n\nNada aqui é transcrição.\n")

	_, unreachable, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(unreachable) != 0 {
		t.Errorf("ordinary prose was reported: %v", unreachable)
	}
}
