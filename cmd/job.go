package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

// report is what bringing in one recording says to whatever called it, and
// the only shape a routine should read. The line a person reads says the same
// things in prose, but a routine deciding whether to file a transcript needs
// the language vote as numbers: a meeting that opened in silence comes back
// translated, fluently and with nothing in the file to say a choice was made.
type report struct {
	Recording      string            `json:"recording_id"`
	Path           string            `json:"path,omitempty"`
	Summary        string            `json:"summary_path,omitempty"`
	Filing         string            `json:"filing,omitempty"`
	Written        bool              `json:"written"`
	Source         string            `json:"source"`
	Renamed        int               `json:"renamed,omitempty"`
	Language       *modal.Language   `json:"language,omitempty"`
	Speakers       map[string]string `json:"speakers,omitempty"`
	Sparse         bool              `json:"sparse,omitempty"`
	CharsPerSecond float64           `json:"chars_per_second,omitempty"`
	Reason         string            `json:"reason,omitempty"`
}

// held is where a report goes when the command that will print it has more to
// say about the same recording. `fetch` answers for the whole errand, and a
// second object describing the transcript inside it would be a second answer.
var held *report

func say(r report) {
	if held != nil {
		*held = r
		return
	}
	if !jsonOut {
		return
	}
	printReport(r)
}

func printReport(r report) {
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	fmt.Println(string(line))
}

// filing is how one recording becomes one file: the format to write, whether
// to decode the audio again, and what the service is told about the recording.
// Both `transcript` and `fetch` fill one in, which is what makes them the same
// work with a different way of choosing the destination.
type filing struct {
	format string
	force  bool
	opts   modal.TranscribeOpts
}

// job is one recording on its way to one file.
type job struct {
	recording api.RecordingSimple
	dest      string
	how       filing
	// written says the file is already there, which leaves only the names in
	// it to bring up to date.
	written bool
	// alone says this is the only recording asked for, and so the one case
	// where a run that changes nothing still owes an answer.
	alone bool
}

// newJob settles whether the file is already there, which is the difference
// between writing a transcript and bringing the names in one up to date.
func newJob(r api.RecordingSimple, dest string, how filing, alone bool) job {
	written := false
	if !how.force {
		_, err := os.Stat(dest)
		written = err == nil
	}
	return job{recording: r, dest: dest, how: how, written: written, alone: alone}
}

func (j job) run(ctx context.Context, whisper *modal.HTTPClient) error {
	if j.written {
		return j.refreshNames(ctx, whisper)
	}
	return j.fromAudio(ctx, whisper)
}

// refreshNames settles again who the voices of a transcript already on disk
// are. It reads the ids the file kept, asks the service who each one is today,
// and rewrites the names it wrote before.
//
// Nothing here listens to audio: the voices of that recording were embedded
// when it was transcribed and are still on the service, so this is a lookup by
// id and a comparison of embeddings. A transcript written before those ids
// asks by its labels, which answers as long as the recording has not been
// separated again since.
func (j job) refreshNames(ctx context.Context, whisper *modal.HTTPClient) error {
	if j.how.format != "md" {
		if j.alone {
			fmt.Fprintf(os.Stderr, "%s is already there; only a markdown transcript can have its names refreshed.\n", j.dest)
		}
		say(report{Recording: j.recording.ID, Path: j.dest, Source: "on disk", Reason: "only markdown is refreshed"})
		return nil
	}

	content, err := os.ReadFile(j.dest)
	if err != nil {
		return fmt.Errorf("reading %s: %w", j.dest, err)
	}
	turns := transcript.ReadTurns(string(content))
	if len(turns) == 0 {
		if j.alone {
			fmt.Fprintf(os.Stderr, "%s holds no turn this tool can read.\n", j.dest)
		}
		return nil
	}

	voices := transcript.ReadVoices(string(content))
	kept := voices != nil
	if !kept {
		voices = labelsAsVoices(turns)
	}

	var keys []string
	for _, ids := range voices {
		keys = append(keys, ids...)
	}
	found, err := whisper.WhoIs(ctx, j.recording.ID, keys)
	if err != nil {
		return fmt.Errorf("asking who the voices are: %w", err)
	}
	names := map[string]string{}
	settledIDs := map[string]string{}
	for _, voice := range found {
		names[voice.Key] = voice.Name
		if voice.Voice != "" {
			names[voice.Voice] = voice.Name
			settledIDs[voice.Key] = voice.Voice
		}
	}
	// A file that asked by its labels learns the ids behind them, and stops
	// depending on a numbering the next transcription would replace. A label
	// nothing answered for is left out rather than written down: the file not
	// knowing which voice a name was is the truth, and an id that never
	// existed would read like a record.
	for written, ids := range voices {
		var known []string
		for _, id := range ids {
			if settled, ok := settledIDs[id]; ok {
				known = append(known, settled)
				continue
			}
			if kept {
				known = append(known, id)
			}
		}
		if len(known) == 0 {
			delete(voices, written)
			continue
		}
		voices[written] = known
	}

	rename := map[string]string{}
	for written, ids := range voices {
		settled, split := agreedName(ids, names)
		if split {
			fmt.Fprintf(os.Stderr, "%s: the voices written as %q are not the same person, so the name stands\n", j.dest, written)
			continue
		}
		if settled != "" && settled != written {
			rename[written] = settled
		}
	}
	if len(rename) == 0 && kept {
		if j.alone {
			fmt.Fprintf(os.Stderr, "%s already names every voice the service can place.\n", j.dest)
		}
		say(report{Recording: j.recording.ID, Path: j.dest, Source: "on disk"})
		return nil
	}

	lines := map[int]string{}
	for _, turn := range turns {
		if name, ok := rename[turn.Speaker]; ok {
			lines[turn.Line] = name
		}
	}
	updated, renamed := transcript.RewriteSpeakers(string(content), lines)
	for written, settled := range rename {
		voices.Rename(written, settled)
	}
	updated = transcript.WriteVoices(updated, voices)

	if err := os.WriteFile(j.dest, []byte(updated), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", j.dest, err)
	}
	if renamed == 0 {
		fmt.Fprintf(os.Stderr, "Wrote down which voice each name in %s stands for\n", j.dest)
		return nil
	}
	fmt.Fprintf(os.Stderr, "Named %d turn(s) in %s\n", renamed, j.dest)
	say(report{Recording: j.recording.ID, Path: j.dest, Source: "on disk", Renamed: renamed})
	return nil
}

// labelsAsVoices reads a transcript written before voices had ids, where the
// label is all there is to ask by.
func labelsAsVoices(turns []transcript.Turn) transcript.VoiceBlock {
	voices := transcript.VoiceBlock{}
	for _, turn := range turns {
		if _, seen := voices[turn.Speaker]; !seen {
			voices[turn.Speaker] = []string{turn.Speaker}
		}
	}
	return voices
}

// agreedName is who a name in the file stands for, when every voice written
// under it agrees. It reports a disagreement rather than picking one: two
// voices under one name mean either a person the diarization split, which
// agrees, or a name given to the wrong voice, which is for a person to settle.
func agreedName(ids []string, names map[string]string) (name string, split bool) {
	for _, id := range ids {
		settled := names[id]
		if settled == "" {
			continue
		}
		if name != "" && settled != name {
			return "", true
		}
		name = settled
	}
	return name, false
}

func (j job) fromAudio(ctx context.Context, whisper *modal.HTTPClient) error {
	result, _, err := whisperTranscribe(ctx, os.Stderr, whisper, j.recording.ID, j.how.opts)
	if err != nil {
		return err
	}
	// A recording of a microphone being put down has no transcript to write,
	// and writing an empty file over the name of one is worse than saying so:
	// the next run would find that file and take the recording for done.
	if len(result.Segments) == 0 {
		fmt.Fprintf(os.Stderr, "%s holds no speech, so nothing was written.\n", j.recording.Name)
		say(report{Recording: j.recording.ID, Source: "service", Reason: "no speech"})
		return nil
	}
	if err := saveWhisperTranscript(result, j.how.format, j.dest); err != nil {
		return err
	}
	if result.Reused {
		fmt.Fprintf(os.Stderr, "\nThe service already had this transcript, so nothing was decoded again.\n")
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\n", j.dest)
	reportLanguage(os.Stderr, result)
	reportUnpolished(os.Stderr, result)
	reportSparse(os.Stderr, result)
	say(report{
		Recording: j.recording.ID, Path: j.dest, Written: true,
		Source:   map[bool]string{true: "kept", false: "decoded"}[result.Reused],
		Language: &result.Language, Speakers: result.Speakers,
		Sparse: result.Sparse, CharsPerSecond: result.CharsPerSecond,
	})

	reportSpeakers(result)
	return nil
}

// reportSpeakers names the voices the run separated, and says when some of
// them are nobody yet. Putting a person to a voice needs somebody who was in
// the room, so it is a thing to be told about rather than a step to be walked
// through here: 'plaud speaker identify' is the page that does it.
func reportSpeakers(result *modal.TranscribeResult) {
	if len(result.Speakers) == 0 {
		return
	}

	var named, unnamed []string
	for label, name := range result.Speakers {
		if label == name {
			unnamed = append(unnamed, label)
			continue
		}
		named = append(named, name)
	}
	sort.Strings(named)
	sort.Strings(unnamed)

	if len(named) > 0 {
		fmt.Fprintf(os.Stderr, "Speakers: %s\n", strings.Join(named, ", "))
	}
	if len(unnamed) > 0 {
		fmt.Fprintf(os.Stderr, "%s belong to nobody the service knows. Run 'plaud speaker identify' to say who they are.\n",
			strings.Join(unnamed, ", "))
	}
}

func applySpeakerNames(segments []transcript.Segment, speakers map[string]string) {
	for i := range segments {
		if name, ok := speakers[segments[i].Speaker]; ok && name != segments[i].Speaker {
			segments[i].Speaker = name
		}
	}
}

// saveTranscript writes segments to the given path in the specified format.
func saveTranscript(segments []transcript.Segment, format, dest string) error {
	if format == "json" {
		data, err := json.MarshalIndent(segments, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling transcript: %w", err)
		}
		return os.WriteFile(dest, data, 0644)
	}
	_, content := transcript.Format(segments, format)
	return os.WriteFile(dest, []byte(content), 0644)
}

// validateFormat rejects an output format none of the writers can produce.
func validateFormat(format string) error {
	switch format {
	case "json", "txt", "srt", "md":
		return nil
	default:
		return fmt.Errorf("unsupported format %q (use json, txt, srt, or md)", format)
	}
}

// readContext takes what describes the recording, from the flag that says
// which of the two it is.
//
// Guessing between the two is what this replaces: a description in Portuguese
// carries a date, a date carries a slash, and a slash read as a path turned
// the sentence into a filename nobody could open. Worse, the guess that went
// the other way polished a transcript against the name of a file.
func readContext(text, path string) (string, error) {
	if path != "" {
		if text != "" {
			return "", fmt.Errorf("--context and --context-file say the same thing twice; pass one")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading the context file: %w", err)
		}
		return string(data), nil
	}
	if _, err := os.Stat(text); err == nil {
		return "", fmt.Errorf("%q is a file that exists, and --context is the description itself — pass --context-file to read it", text)
	}
	return text, nil
}
