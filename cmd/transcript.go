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
	trFilter    recordingFilter
	trAll       bool
	trForce     bool
	trOutputDir string
	trFormat    string
	trContext   string
	trLanguage  string
	trIdentify  bool
	trOpts      modal.TranscribeOpts
)

var transcriptCmd = &cobra.Command{
	Use:     "transcript [id]",
	Aliases: []string{"transcribe"},
	Short:   "Write the transcript of a recording, or of many",
	Long: `Put the text of a recording on disk.

--context is required and takes any file describing the recording: an agenda,
prep notes, a briefing. It is what settles how the names in it are spelt, and
transcripts of the same people drift apart without it.

A transcript that already exists is reused. --force transcribes the audio
again, which is what to reach for when the one on record is an old one.

Naming a recording does one. A filter, or --all, does every recording it keeps,
skipping the ones already written unless --force says otherwise.

Examples:
  plaud transcript abc123 --context ./meeting-prep.md
  plaud transcript abc123 --context ./prep.md --identify
  plaud transcript abc123 --context ./prep.md --force
  plaud transcript --since 2026-08-01 --context ./briefing.md --output-dir ./recordings
  plaud transcript --tag cliente --context ./briefing.md --format srt`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := validateFormat(trFormat); err != nil {
			return err
		}
		if err := os.MkdirAll(trOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		recordings, err := chooseRecordings(ctx, args, trFilter, trAll)
		if err != nil {
			return err
		}
		if len(recordings) == 0 {
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		}

		trOpts.Language = trLanguage
		data, err := os.ReadFile(trContext)
		if err != nil {
			return fmt.Errorf("reading context file: %w", err)
		}
		trOpts.ContextDoc = string(data)

		pending := plan(recordings)
		if len(pending) == 0 {
			fmt.Fprintln(os.Stderr, "Every recording already has its transcript here. Pass --force to write them again.")
			return nil
		}

		// Resolve the service once: a missing credential should fail before
		// the first recording rather than once per recording.
		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		var failed int
		for _, job := range pending {
			if len(pending) > 1 {
				fmt.Fprintf(os.Stderr, "\n%s\n", job.recording.Name)
			}
			if err := job.run(ctx, whisper); err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", job.recording.Name, err)
				failed++
			}
		}

		if len(pending) > 1 {
			fmt.Fprintf(os.Stderr, "\n%d written, %d failed\n", len(pending)-failed, failed)
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
}

// plan drops the recordings whose transcript is already on disk. The directory
// is the record of what has been done, so there is no state file to go stale.
func plan(recordings []api.RecordingSimple) []job {
	var pending []job
	for _, r := range recordings {
		dest := filepath.Join(trOutputDir, transcript.BaseName(r.Name, r.StartTime)+transcript.Ext(trFormat))
		if !trForce {
			if _, err := os.Stat(dest); err == nil {
				continue
			}
		}
		pending = append(pending, job{recording: r, dest: dest})
	}
	return pending
}

func (j job) run(ctx context.Context, whisper *modal.HTTPClient) error {
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
	f.StringVar(&trFormat, "format", "md", "output format: json, txt, srt, md")
	f.StringVar(&trLanguage, "language", "", "force a language code (e.g. pt, en), empty to detect it")
	f.StringVar(&trContext, "context", "", "file describing the recording (agenda, notes, briefing); settles how names are spelt")
	f.BoolVar(&trIdentify, "identify", false, "ask who the unrecognised voices are once the transcript is written")
	must(transcriptCmd.MarkFlagRequired("context"))
	rootCmd.AddCommand(transcriptCmd)
}
