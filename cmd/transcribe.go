package cmd

import (
	"bufio"
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
	trOutputDir  string
	trFormat     string
	trOptions    string
	trContext    string
	trCompactGap int
	trLanguage   string
	trIdentify   bool
)

var transcribeCmd = &cobra.Command{
	Use:   "transcribe <id>",
	Short: "Transcribe a recording using Whisper via Modal",
	Long: `Download a recording's audio and transcribe it using a Whisper model
deployed on Modal. By default enables diarization, polishing, and compaction.

Requires Modal credentials configured via 'plaud modal-auth'.

Examples:
  plaud transcribe abc123
  plaud transcribe abc123 --context ./meeting-prep.md
  plaud transcribe abc123 --options no-polish
  plaud transcribe abc123 --options no-polish,no-compact
  plaud transcribe abc123 --options no-diarize,no-polish,no-compact
  plaud transcribe abc123 --compact-gap 3000 --context ./prep.md
  plaud transcribe abc123 --identify
  plaud transcribe abc123 --options no-speaker-recognition
  plaud transcribe abc123 --format srt --output-dir ./transcripts`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		if err := validateFormat(trFormat); err != nil {
			return err
		}

		if err := os.MkdirAll(trOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		baseName := transcript.SanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatEpochMs(detail.StartTime), " ", "_")

		opts, err := parseTranscribeOptions(trOptions)
		if err != nil {
			return err
		}
		opts.CompactGap = trCompactGap
		opts.Language = trLanguage

		if trContext != "" {
			data, err := os.ReadFile(trContext)
			if err != nil {
				return fmt.Errorf("reading context file: %w", err)
			}
			opts.ContextDoc = string(data)
		}

		result, audioData, err := whisperTranscribe(ctx, os.Stderr, whisper, id, opts)
		if err != nil {
			return err
		}

		dest := filepath.Join(trOutputDir, baseName+"_whisper"+transcript.Ext(trFormat))
		if err := saveWhisperTranscript(result, trFormat, dest); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\nSaved to %s\n", dest)

		if len(result.Speakers) == 0 {
			return nil
		}

		var recognized []string
		for id, name := range result.Speakers {
			if id != name {
				recognized = append(recognized, name)
			}
		}

		if !trIdentify {
			var names []string
			for id, name := range result.Speakers {
				if id == name {
					names = append(names, id)
				} else {
					names = append(names, name)
				}
			}
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
				// The browser asks for a name; the convention is to type the
				// same form a transcript shows, "First Last (Company)".
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
		if err := saveWhisperTranscript(result, trFormat, dest); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to update transcript: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Updated %s\n", dest)
		}

		return nil
	},
}

// parseTranscribeOptions reads the comma-separated disable flags of --options.
func parseTranscribeOptions(s string) (modal.TranscribeOpts, error) {
	opts := modal.TranscribeOpts{
		Diarize:            true,
		Polish:             true,
		Compact:            true,
		SpeakerRecognition: true,
	}
	if s == "" {
		return opts, nil
	}
	for _, opt := range strings.Split(s, ",") {
		switch strings.TrimSpace(opt) {
		case "no-diarize":
			opts.Diarize = false
		case "no-polish":
			opts.Polish = false
		case "no-compact":
			opts.Compact = false
		case "no-speaker-recognition":
			opts.SpeakerRecognition = false
		default:
			return opts, fmt.Errorf("unknown option %q (valid: no-diarize, no-polish, no-compact, no-speaker-recognition)", opt)
		}
	}
	// Both compaction and speaker recognition read the speaker of a segment,
	// which only diarization fills in.
	if !opts.Diarize {
		opts.Compact = false
		opts.SpeakerRecognition = false
	}
	return opts, nil
}

// applySpeakerNames replaces SPEAKER_XX tags in segments with real names from the speakers map.
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

func init() {
	transcribeCmd.Flags().StringVar(&trOutputDir, "output-dir", ".", "output directory")
	transcribeCmd.Flags().StringVar(&trFormat, "format", "md", "output format: json, txt, srt, md")
	transcribeCmd.Flags().StringVar(&trOptions, "options", "", "comma-separated disable flags: no-diarize, no-polish, no-compact, no-speaker-recognition")
	transcribeCmd.Flags().StringVar(&trContext, "context", "", "path to meeting context file (agenda, notes) for better hotwords and polishing")
	transcribeCmd.Flags().IntVar(&trCompactGap, "compact-gap", 2000, "max silence gap in ms before starting a new paragraph")
	transcribeCmd.Flags().StringVar(&trLanguage, "language", "", "force language code (e.g. pt, en), empty for auto-detect")
	transcribeCmd.Flags().BoolVar(&trIdentify, "identify", false, "interactively identify unrecognized speakers after transcription")
	rootCmd.AddCommand(transcribeCmd)
}
