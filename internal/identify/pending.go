package identify

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

// label is a voice diarization separated and nobody has put a person to.
var label = regexp.MustCompile(`^SPEAKER_\d+$`)

// Voice is one unnamed voice, in the transcript that carries it.
//
// ID is what the service is asked about: the id the voice was given when the
// recording was separated, or the label itself for a file written before those
// ids, which answers only while that run is still the current one.
type Voice struct {
	Recording string   `json:"recording"`
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	File      string   `json:"file"`
	Title     string   `json:"title"`
	Samples   []Sample `json:"samples"`
}

// Sample is a stretch of the recording where that voice is the one speaking.
type Sample struct {
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
	Text     string  `json:"text"`
}

// maxSamples is how many stretches of one voice are offered. Three long turns
// place a voice; a list of every turn is a transcript, which is the file.
const maxSamples = 3

// Pending finds every voice still called SPEAKER_nn in the transcripts under a
// directory.
//
// A transcript says which recording it came from and which voice each name in
// it stands for, so this needs nothing but the files: no account, no service,
// and no memory of what was transcribed when.
// Unreachable is a transcript holding unnamed voices that cannot be helped:
// it does not say which recording it came from, so there is nothing to ask the
// service about. Fetching it again stamps the recording in and makes it
// reachable, which costs a request and decodes nothing.
func Pending(root string) ([]Voice, []string, error) {
	var found []Voice
	var unreachable []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		voices, orphaned := pendingIn(string(content), path)
		found = append(found, voices...)
		if orphaned {
			unreachable = append(unreachable, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(unreachable)

	sort.Slice(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		return found[i].Label < found[j].Label
	})
	return found, unreachable, nil
}

// pendingIn reads one file, and reports whether it holds voices nobody can act
// on because it never wrote down which recording it came from.
func pendingIn(content, path string) (voices []Voice, unreachable bool) {
	turns := transcript.ReadTurns(content)
	recording := transcript.ReadField(content, transcript.RecordingKey)
	if recording == "" {
		for _, turn := range turns {
			if label.MatchString(turn.Speaker) {
				return nil, true
			}
		}
		return nil, false
	}
	written := transcript.ReadVoices(content)

	var found []Voice
	for name, ids := range written {
		if !label.MatchString(name) {
			continue
		}
		// One label holding several ids is a person diarization split. Naming
		// the first teaches one of the voices; the rest come back next time.
		for _, id := range ids {
			found = append(found, Voice{
				Recording: recording,
				ID:        id,
				Label:     name,
				File:      path,
				Title:     transcript.ReadField(content, "title"),
				Samples:   samplesOf(name, turns, content),
			})
		}
	}
	if written == nil {
		// A transcript written before the ids has only its labels to ask by.
		seen := map[string]bool{}
		for _, turn := range turns {
			if !label.MatchString(turn.Speaker) || seen[turn.Speaker] {
				continue
			}
			seen[turn.Speaker] = true
			found = append(found, Voice{
				Recording: recording, ID: turn.Speaker, Label: turn.Speaker,
				File: path, Title: transcript.ReadField(content, "title"),
				Samples: samplesOf(turn.Speaker, turns, content),
			})
		}
	}
	return found, false
}

// samplesOf picks the longest stretches one voice holds, which are the ones
// carrying enough speech to recognise somebody by.
func samplesOf(speaker string, turns []transcript.Turn, content string) []Sample {
	lines := strings.Split(content, "\n")

	var held []transcript.Turn
	for _, turn := range turns {
		if turn.Speaker == speaker {
			held = append(held, turn)
		}
	}
	sort.SliceStable(held, func(i, j int) bool {
		return held[i].EndMS-held[i].StartMS > held[j].EndMS-held[j].StartMS
	})
	if len(held) > maxSamples {
		held = held[:maxSamples]
	}
	sort.SliceStable(held, func(i, j int) bool { return held[i].StartMS < held[j].StartMS })

	samples := make([]Sample, 0, len(held))
	for _, turn := range held {
		samples = append(samples, Sample{
			StartSec: float64(turn.StartMS) / 1000,
			EndSec:   float64(turn.EndMS) / 1000,
			Text:     said(lines, turn.Line),
		})
	}
	return samples
}

// said is what a turn's line is followed by, which is the speech itself.
func said(lines []string, header int) string {
	var spoken []string
	for i := header + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if len(spoken) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "**") {
			break
		}
		spoken = append(spoken, line)
	}
	text := strings.Join(spoken, " ")
	if len(text) > 300 {
		text = strings.TrimSpace(text[:300]) + "…"
	}
	return text
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".next", "dist", "build":
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}
