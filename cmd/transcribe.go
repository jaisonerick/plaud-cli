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
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	trOutputDir          string
	trFormat             string
	trOptions            string
	trContext            string
	trCompactGap         int
	trLanguage           string
	trIdentify bool
)

var transcribeCmd = &cobra.Command{
	Use:   "transcribe <id>",
	Short: "Transcribe a recording using Whisper via Modal",
	Long: `Download a recording's audio and transcribe it using a Whisper model
deployed on Modal. By default enables diarization, polishing, and compaction.

Requires environment variables:
  MODAL_TOKEN_ID       Modal authentication token ID
  MODAL_TOKEN_SECRET   Modal authentication token secret

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

		// Check Modal configuration (env vars take priority, then saved config)
		modalCfg := modal.LoadConfig(cfg.ModalTokenID, cfg.ModalTokenSecret)
		if modalCfg == nil {
			return fmt.Errorf("Modal not configured. Run 'plaud modal-auth' or set MODAL_TOKEN_ID and MODAL_TOKEN_SECRET environment variables")
		}

		// Validate format
		switch trFormat {
		case "json", "txt", "srt", "md":
			// ok
		default:
			return fmt.Errorf("unsupported format %q (use json, txt, srt, or md)", trFormat)
		}

		// Ensure output directory exists
		if err := os.MkdirAll(trOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		// Fetch recording details
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		baseName := transcript.SanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatEpochMs(detail.StartTime), " ", "_")

		// Download audio
		fmt.Fprint(os.Stderr, "Downloading audio... ")
		tempURL, err := client.GetTempURL(ctx, id)
		if err != nil {
			return fmt.Errorf("getting download URL: %w", err)
		}

		audioData, err := client.FetchFile(ctx, tempURL)
		if err != nil {
			return fmt.Errorf("downloading audio: %w", err)
		}
		fmt.Fprintf(os.Stderr, "done (%d bytes)\n", len(audioData))

		// Parse options
		diarize, polish, compact, speakerRecognition := true, true, true, true
		if trOptions != "" {
			for _, opt := range strings.Split(trOptions, ",") {
				switch strings.TrimSpace(opt) {
				case "no-diarize":
					diarize = false
				case "no-polish":
					polish = false
				case "no-compact":
					compact = false
				case "no-speaker-recognition":
					speakerRecognition = false
				default:
					return fmt.Errorf("unknown option %q (valid: no-diarize, no-polish, no-compact, no-speaker-recognition)", opt)
				}
			}
		}
		// compact requires diarize
		if !diarize {
			compact = false
		}

		// Read context file if provided
		var contextDoc string
		if trContext != "" {
			data, err := os.ReadFile(trContext)
			if err != nil {
				return fmt.Errorf("reading context file: %w", err)
			}
			contextDoc = string(data)
		}

		// Transcribe via Modal
		fmt.Fprint(os.Stderr, "Transcribing")
		if diarize {
			fmt.Fprint(os.Stderr, " with speaker diarization")
		}
		if polish {
			fmt.Fprint(os.Stderr, " + polish")
		}
		if compact {
			fmt.Fprint(os.Stderr, " + compact")
		}
		fmt.Fprint(os.Stderr, "... ")

		result, err := modal.Transcribe(ctx, modalCfg, audioData, modal.TranscribeOpts{
			Diarize:            diarize,
			Polish:             polish,
			Compact:            compact,
			CompactGap:         trCompactGap,
			Language:           trLanguage,
			ContextDoc:         contextDoc,
			SpeakerRecognition: speakerRecognition,
		})
		if err != nil {
			return fmt.Errorf("transcribing: %w", err)
		}
		fmt.Fprintf(os.Stderr, "done (%d segments)\n", len(result.Segments))

		// Save result
		var dest string
		if trFormat == "json" {
			dest = filepath.Join(trOutputDir, baseName+"_whisper.json")
			data, err := json.MarshalIndent(result.Segments, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling transcript: %w", err)
			}
			if err := os.WriteFile(dest, data, 0644); err != nil {
				return fmt.Errorf("writing transcript: %w", err)
			}
		} else {
			ext, content := transcript.Format(result.Segments, trFormat)
			dest = filepath.Join(trOutputDir, baseName+"_whisper"+ext)
			if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing transcript: %w", err)
			}
		}

		fmt.Printf("Transcript saved to %s\n", dest)

		// Print audio ID and speaker mapping
		if result.AudioID != "" {
			fmt.Printf("Audio ID: %s\n", result.AudioID)
		}
		if len(result.Speakers) > 0 {
			parts := make([]string, 0, len(result.Speakers))
			for k, v := range result.Speakers {
				parts = append(parts, fmt.Sprintf("%s → %s", k, v))
			}
			fmt.Printf("Speakers: %s\n", strings.Join(parts, ", "))
		}

		// Interactive speaker identification
		if trIdentify {
			unresolved := identify.UnresolvedSpeakers(result.Speakers)
			if len(unresolved) > 0 {
				var recognized []string
				for k, v := range result.Speakers {
					if k != v {
						recognized = append(recognized, v)
					}
				}
				if len(recognized) > 0 {
					fmt.Fprintf(os.Stderr, "\nRecognized: %s\n", strings.Join(recognized, ", "))
				}
				fmt.Fprintf(os.Stderr, "%d unidentified speaker(s): %s\n", len(unresolved), strings.Join(unresolved, ", "))
				fmt.Fprintf(os.Stderr, "\nOpen browser to identify speakers? [Y/n] ")

				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(strings.ToLower(line))

				if line == "" || line == "y" || line == "yes" {
					idResult, err := identify.RunServer(ctx, identify.Config{
						AudioData: audioData,
						AudioID:   result.AudioID,
						Speakers:  result.Speakers,
						Segments:  result.Segments,
					})
					if err != nil {
						return fmt.Errorf("speaker identification: %w", err)
					}

					var wg sync.WaitGroup
					for speakerID, name := range idResult.Names {
						wg.Add(1)
						go func(sid, n string) {
							defer wg.Done()
							if err := modal.SetSpeakerName(ctx, modalCfg, result.AudioID, sid, n); err != nil {
								fmt.Fprintf(os.Stderr, "Warning: failed to set name for %s: %v\n", sid, err)
								return
							}
							fmt.Printf("Speaker %q registered from audio %s/%s\n", n, result.AudioID, sid)
						}(speakerID, name)
					}
					wg.Wait()
				}
			} else {
				fmt.Fprintf(os.Stderr, "All speakers identified.\n")
			}
		}

		return nil
	},
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
