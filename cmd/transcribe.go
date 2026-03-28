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
	"github.com/jaisonerick/plaud-cli/internal/progress"
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

		// Check Modal configuration
		httpClient := modal.LoadHTTPClient(cfg.ModalTokenID, cfg.ModalTokenSecret, cfg.ModalEndpointURL)
		if httpClient == nil {
			return fmt.Errorf("Modal not configured. Run 'plaud modal-auth' or set MODAL_TOKEN_ID, MODAL_TOKEN_SECRET, and MODAL_ENDPOINT_URL environment variables")
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

		// Set up progress tracker with client-side stages
		tracker := progress.NewTracker(os.Stderr, []progress.StageDef{
			{ID: "download", Label: "Downloading audio"},
			{ID: "upload", Label: "Waiting for server"},
		})

		// Phase 1: Download audio
		tracker.Update(progress.Event{Stage: "download", Status: "started"})
		tempURL, err := client.GetTempURL(ctx, id)
		if err != nil {
			return fmt.Errorf("getting download URL: %w", err)
		}

		audioData, err := client.FetchFile(ctx, tempURL, func(received, total int64) {
			if total > 0 {
				pct := received * 100 / total
				tracker.Update(progress.Event{
					Stage:  "download",
					Status: "progress",
					Detail: fmt.Sprintf("%d%%  %.1f MB", pct, float64(received)/1e6),
				})
			}
		})
		if err != nil {
			return fmt.Errorf("downloading audio: %w", err)
		}
		tracker.Update(progress.Event{Stage: "download", Status: "done", Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})

		// Phase 2: Wait for server
		// Covers upload, cold start, and initial server handshake.
		// Ends when the first SSE event (init) arrives.
		tracker.Update(progress.Event{Stage: "upload", Status: "started"})

		events, errCh := httpClient.TranscribeStream(ctx, audioData, modal.TranscribeOpts{
			Diarize:            diarize,
			Polish:             polish,
			Compact:            compact,
			CompactGap:         trCompactGap,
			Language:           trLanguage,
			ContextDoc:         contextDoc,
			SpeakerRecognition: speakerRecognition,
		}, modal.StreamCallbacks{})

		var result *modal.TranscribeResult
		for evt := range events {
			switch evt.Type {
			case "init":
				// First server event — server is ready
				tracker.Update(progress.Event{Stage: "upload", Status: "done"})
				// Insert server stages, then save
				tracker.AddStages(evt.Stages)
				tracker.AddStages([]progress.StageDef{{ID: "save", Label: "Saving transcript"}})

			case "update":
				e := progress.Event{
					Stage:  evt.Stage,
					Status: evt.Status,
				}
				if evt.Detail != nil {
					e.Detail = *evt.Detail
				}
				if evt.Progress != nil {
					e.Current = evt.Progress.Current
					e.Total = evt.Progress.Total
				}
				tracker.Update(e)

			case "result":
				result = &modal.TranscribeResult{
					AudioID:  evt.AudioID,
					Segments: evt.Segments,
					Speakers: evt.Speakers,
				}

			case "error":
				tracker.Wait()
				return fmt.Errorf("transcription failed at %s: %s", evt.Stage, evt.Message)
			}
		}
		if err := <-errCh; err != nil {
			tracker.Wait()
			return fmt.Errorf("stream error: %w", err)
		}

		if result == nil {
			tracker.Wait()
			return fmt.Errorf("no result received from server")
		}

		// Phase 3: Save result
		tracker.Update(progress.Event{Stage: "save", Status: "started"})

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

		tracker.Update(progress.Event{Stage: "save", Status: "done"})
		tracker.Wait()

		// Summary
		fmt.Fprintf(os.Stderr, "\nSaved to %s\n", dest)

		// Speaker identification
		if len(result.Speakers) > 0 && trIdentify {
			unresolved := identify.UnresolvedSpeakers(result.Speakers)

			// Show recognized speakers
			var recognized []string
			for k, v := range result.Speakers {
				if k != v {
					recognized = append(recognized, v)
				}
			}
			if len(recognized) > 0 {
				fmt.Fprintf(os.Stderr, "Recognized: %s\n", strings.Join(recognized, ", "))
			}

			if len(unresolved) > 0 {
				fmt.Fprintf(os.Stderr, "Unidentified: %s\n", strings.Join(unresolved, ", "))
				fmt.Fprintf(os.Stderr, "\nOpen browser to identify? [Y/n] ")

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

					if len(idResult.Names) > 0 {
						fmt.Fprintf(os.Stderr, "\n")
						var wg sync.WaitGroup
						for speakerID, name := range idResult.Names {
							wg.Add(1)
							go func(sid, n string) {
								defer wg.Done()
								if err := httpClient.SetSpeakerName(ctx, result.AudioID, sid, n); err != nil {
									fmt.Fprintf(os.Stderr, "  Warning: failed to register %s: %v\n", sid, err)
									return
								}
								fmt.Fprintf(os.Stderr, "  Registered %q (%s)\n", n, sid)
							}(speakerID, name)
						}
						wg.Wait()

						// Update speakers map and re-save transcript with real names
						for sid, name := range idResult.Names {
							result.Speakers[sid] = name
						}
						applySpeakerNames(result.Segments, result.Speakers)
						if err := saveTranscript(result.Segments, trFormat, dest); err != nil {
							fmt.Fprintf(os.Stderr, "  Warning: failed to update transcript: %v\n", err)
						} else {
							fmt.Fprintf(os.Stderr, "  Updated %s\n", dest)
						}
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "All speakers identified: %s\n", strings.Join(recognized, ", "))
			}
		} else if len(result.Speakers) > 0 {
			// Not using --identify, just list speakers
			var names []string
			for k, v := range result.Speakers {
				if k != v {
					names = append(names, v)
				} else {
					names = append(names, k)
				}
			}
			fmt.Fprintf(os.Stderr, "Speakers: %s\n", strings.Join(names, ", "))
		}

		return nil
	},
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
