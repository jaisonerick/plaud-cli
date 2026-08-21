package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/identify"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	trFilter      recordingFilter
	trAll         bool
	trForce       bool
	trOutputDir   string
	trFormat      string
	trContext     string
	trContextFile string
	trLanguage    string
	trInto        string
	trIdentify    bool
	trOpts        modal.TranscribeOpts
)

var transcriptCmd = &cobra.Command{
	Use:     "transcript [id]",
	Aliases: []string{"transcribe"},
	Short:   "Write the transcript of a recording, or of many",
	Long: `Put the text of a recording on disk.

One of --context or --context-file is required. --context-file reads a file
describing the recording — an agenda, prep notes, a briefing; --context takes
that description written out. It is what settles how the names in it are
spelt, and transcripts of the same people drift apart without it. A document covering the whole engagement serves every
recording in it: what the polisher reads out of it is who the people are and
how their names and systems are spelt, which the subject of one meeting
barely changes.

A transcript that already exists is reused. --force transcribes the audio
again, which is what to reach for when the one on record is an old one.

A transcript already written here is not skipped: the file keeps the id of each
voice in it, so a name settled since replaces the one written at the time. That
is a lookup and a comparison of voices already on the service — no audio is
fetched and nothing is decoded.

Naming a recording does one. A filter, or --all, does every recording it keeps.

--into writes one recording to exactly that file, whatever it is called, and
refreshes the names in it when it is already there.

Examples:
  plaud transcript abc123 --context-file ./meeting-prep.md
  plaud transcript abc123 --context "Vexia and CERC on payments; Éricles Bento, Zeni"
  plaud transcript abc123 --context-file ./prep.md --identify
  plaud transcript abc123 --context-file ./prep.md --force
  plaud transcript --since 2026-08-01 --context-file ./briefing.md --output-dir ./recordings
  plaud transcript --tag cliente --context-file ./briefing.md --format srt`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := validateFormat(trFormat); err != nil {
			return err
		}
		outputDir := trOutputDir
		if trInto != "" {
			outputDir = filepath.Dir(trInto)
		}
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		// Before anything is fetched: a context that cannot be read is worth
		// saying so about while it still costs nothing.
		contextDoc, err := readContext(trContext, trContextFile)
		if err != nil {
			return err
		}
		trOpts.ContextDoc = contextDoc
		trOpts.Language = trLanguage

		recordings, err := chooseRecordings(ctx, args, trFilter, trAll)
		if err != nil {
			return err
		}
		if len(recordings) == 0 {
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		}

		if trInto != "" && len(recordings) > 1 {
			return fmt.Errorf("--into names one file, and this matched %d recordings", len(recordings))
		}

		pending := plan(recordings)

		// Resolve the service once: a missing credential should fail before
		// the first recording rather than once per recording.
		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		var toWrite int
		for _, job := range pending {
			if !job.written {
				toWrite++
			}
		}
		if toWrite > 1 {
			fmt.Fprintf(os.Stderr, "Transcribing %d recording(s); the %d already on disk have their voices checked against the audio.\n", toWrite, len(pending)-toWrite)
		}

		var wrote, failed int
		for _, job := range pending {
			if !job.written && len(pending) > 1 {
				fmt.Fprintf(os.Stderr, "\n%s\n", job.recording.Name)
			}
			if err := job.run(ctx, whisper); err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", job.recording.Name, err)
				failed++
				continue
			}
			if !job.written {
				wrote++
			}
		}

		if len(pending) > 1 {
			fmt.Fprintf(os.Stderr, "\n%d written, %d already on disk, %d failed\n", wrote, len(pending)-toWrite, failed)
		}
		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

// job is one recording on its way to one file.
type job struct {
	recording api.RecordingSimple
	dest      string
	// written says the file is already there, which leaves only the names in
	// it to bring up to date.
	written bool
	// alone says this is the only recording asked for, and so the one case
	// where a run that changes nothing still owes an answer.
	alone bool
}

// plan puts each recording next to the file it belongs in. A transcript
// already on disk is not skipped: its text is settled, but a voice named after
// it was written is still called SPEAKER_03 in it.
func plan(recordings []api.RecordingSimple) []job {
	var pending []job
	for _, r := range recordings {
		dest := filepath.Join(trOutputDir, transcript.BaseName(r.Name, r.StartTime)+transcript.Ext(trFormat))
		if trInto != "" {
			dest = trInto
		}
		written := false
		if !trForce {
			_, err := os.Stat(dest)
			written = err == nil
		}
		pending = append(pending, job{recording: r, dest: dest, written: written, alone: len(recordings) == 1})
	}
	return pending
}

func (j job) run(ctx context.Context, whisper *modal.HTTPClient) error {
	if j.written {
		return j.refreshNames(ctx, whisper)
	}
	if !trForce && j.recording.HasTranscript {
		written, err := j.fromRecord(ctx)
		if err != nil {
			return err
		}
		if written {
			fmt.Fprintf(os.Stderr, "Saved to %s\n", j.dest)
			return nil
		}
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
	if trFormat != "md" {
		if j.alone {
			fmt.Fprintf(os.Stderr, "%s is already there; only a markdown transcript can have its names refreshed.\n", j.dest)
		}
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

// fromRecord writes the transcript already on record. It reports whether it
// found one: an account can claim a transcript and serve no link to it.
func (j job) fromRecord(ctx context.Context) (bool, error) {
	detail, err := client.GetDetail(ctx, j.recording.ID)
	if err != nil {
		return false, fmt.Errorf("fetching recording details: %w", err)
	}
	url := detail.TranscriptURL()
	if url == "" {
		return false, nil
	}

	data, err := client.FetchGzipped(ctx, url)
	if err != nil {
		return false, fmt.Errorf("downloading transcript: %w", err)
	}
	segments, err := transcript.Parse(data)
	if err != nil {
		return false, fmt.Errorf("parsing transcript: %w", err)
	}
	return true, saveTranscript(segments, trFormat, j.dest)
}

func (j job) fromAudio(ctx context.Context, whisper *modal.HTTPClient) error {
	result, audioData, err := whisperTranscribe(ctx, os.Stderr, whisper, j.recording.ID, trOpts)
	if err != nil {
		return err
	}
	// A recording of a microphone being put down has no transcript to write,
	// and writing an empty file over the name of one is worse than saying so:
	// the next run would find that file and take the recording for done.
	if len(result.Segments) == 0 {
		fmt.Fprintf(os.Stderr, "%s holds no speech, so nothing was written.\n", j.recording.Name)
		return nil
	}
	if err := saveWhisperTranscript(result, trFormat, j.dest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nSaved to %s\n", j.dest)
	reportLanguage(os.Stderr, result)
	reportUnpolished(os.Stderr, result)
	reportSparse(os.Stderr, result)

	return reportSpeakers(ctx, whisper, result, audioData, j)
}

// reportSpeakers names the voices the run separated, and offers to put people
// to the ones nothing recognised when a person is there to say who they are.
func reportSpeakers(ctx context.Context, whisper *modal.HTTPClient, result *modal.TranscribeResult, audioData []byte, j job) error {
	if len(result.Speakers) == 0 {
		return nil
	}

	var recognized, names []string
	for id, name := range result.Speakers {
		names = append(names, name)
		if id != name {
			recognized = append(recognized, name)
		}
	}

	if !trIdentify {
		fmt.Fprintf(os.Stderr, "Speakers: %s\n", strings.Join(names, ", "))
		return nil
	}

	if len(recognized) > 0 {
		fmt.Fprintf(os.Stderr, "Recognized: %s\n", strings.Join(recognized, ", "))
	}
	unresolved := identify.UnresolvedSpeakers(result.Speakers)
	if len(unresolved) == 0 {
		fmt.Fprintf(os.Stderr, "All speakers identified: %s\n", strings.Join(recognized, ", "))
		return nil
	}

	fmt.Fprintf(os.Stderr, "Unidentified: %s\n", strings.Join(unresolved, ", "))
	fmt.Fprintf(os.Stderr, "\nOpen browser to identify? [Y/n] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "", "y", "yes":
	default:
		return nil
	}

	return runIdentify(ctx, whisper, result, audioData, j)
}

// runIdentify opens the browser that asks who each unrecognised voice is,
// registers the answers with the service, and rewrites the file with them.
func runIdentify(ctx context.Context, whisper *modal.HTTPClient, result *modal.TranscribeResult, audioData []byte, j job) error {
	idResult, err := identify.RunServer(ctx, identify.Config{
		AudioData: audioData,
		AudioID:   result.AudioID,
		Speakers:  result.Speakers,
		Segments:  result.Segments,
	})
	if err != nil {
		return fmt.Errorf("speaker identification: %w", err)
	}
	if len(idResult.Names) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n")
	var wg sync.WaitGroup
	for speakerID, name := range idResult.Names {
		wg.Add(1)
		go func(sid, typed string) {
			defer wg.Done()
			// The browser asks for a name; the convention is to type the same
			// form a transcript shows, "First Last (Company)".
			person, company := speaker.ParseDisplay(typed)
			if company == "" {
				fmt.Fprintf(os.Stderr, "  Skipped %s: write %q as \"First Last (Company)\"\n", sid, typed)
				return
			}
			saved, err := whisper.NameSpeaker(ctx, result.AudioID, sid, person, company, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not register %s: %v\n", sid, err)
				return
			}
			fmt.Fprintf(os.Stderr, "  Registered %s (%s)\n", saved.Display(), sid)
		}(speakerID, name)
	}
	wg.Wait()

	for sid, name := range idResult.Names {
		result.Speakers[sid] = name
	}
	applySpeakerNames(result.Segments, result.Speakers)
	if err := saveWhisperTranscript(result, trFormat, j.dest); err != nil {
		return fmt.Errorf("updating the transcript with the names: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Updated %s\n", j.dest)
	return nil
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

// must fails the build of a command rather than the run of it: a flag that
// cannot be marked required is a mistake in this file, not in a call.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	f := transcriptCmd.Flags()
	addFilterFlags(transcriptCmd, &trFilter)
	f.BoolVar(&trAll, "all", false, "every recording in the account")
	f.BoolVar(&trForce, "force", false, "transcribe the audio again instead of reusing a transcript that exists")
	f.StringVar(&trOutputDir, "output-dir", ".", "where the transcripts are written")
	f.StringVar(&trInto, "into", "", "write one recording to exactly this file, refreshing the names when it is there")
	f.StringVar(&trFormat, "format", "md", "output format: json, txt, srt, md")
	f.StringVar(&trLanguage, "language", "", "force a language code (e.g. pt, en), empty to detect it")
	f.StringVar(&trContext, "context", "", "what the recording is about, written out; settles how names are spelt")
	f.StringVar(&trContextFile, "context-file", "", "a file describing the recording, read as the context")
	f.BoolVar(&trIdentify, "identify", false, "ask who the unrecognised voices are once the transcript is written")
	transcriptCmd.MarkFlagsOneRequired("context", "context-file")
	rootCmd.AddCommand(transcriptCmd)
}
