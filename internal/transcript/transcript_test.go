package transcript

import (
	"strings"
	"testing"
	"time"
)

// A name that reaches the filesystem has to survive Windows, tar and a shell,
// and the two halves it is built from both carry characters that do not.
func TestBaseNameCarriesNoCharacterAFilesystemRefuses(t *testing.T) {
	started := time.Date(2026, 8, 17, 14, 5, 0, 0, time.Local).UnixMilli()

	for _, title := range []string{
		"08-18 Consulta: Otimização de Contas a Pagar CERC/VEX",
		"2026-08-13 14:02:13",
		`weird "quoted" <name> | *?\`,
	} {
		got := BaseName(title, started)
		if strings.ContainsAny(got, `/\:*?"<>|`) {
			t.Errorf("BaseName(%q) = %q, which a filesystem refuses", title, got)
		}
	}
}

func TestBaseNameEndsWithTheStartTimeSortably(t *testing.T) {
	started := time.Date(2026, 8, 17, 14, 5, 0, 0, time.Local).UnixMilli()

	if got, want := BaseName("Weekly", started), "Weekly_2026-08-17_14-05"; got != want {
		t.Errorf("BaseName = %q, want %q", got, want)
	}
}

func TestBaseNameNamesAnUntitledRecording(t *testing.T) {
	started := time.Date(2026, 8, 17, 14, 5, 0, 0, time.Local).UnixMilli()

	if got, want := BaseName("", started), "recording_2026-08-17_14-05"; got != want {
		t.Errorf("BaseName = %q, want %q", got, want)
	}
}
