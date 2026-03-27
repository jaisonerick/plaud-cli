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
			{ID: "connect", Label: "Waiting for server"},
			{ID: "upload", Label: "Uploading audio"},
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

		// Phase 2: Connect + upload + server streaming
		tracker.Update(progress.Event{Stage: "connect", Status: "started"})

		sizeMB := fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)
		events, errCh := httpClient.TranscribeStream(ctx, audioData, modal.TranscribeOpts{
			Diarize:            diarize,
			Polish:             polish,
			Compact:            compact,
			CompactGap:         trCompactGap,
			Language:           trLanguage,
			ContextDoc:         contextDoc,
			SpeakerRecognition: speakerRecognition,
		}, modal.StreamCallbacks{
			OnUploadStart: func() {
				tracker.Update(progress.Event{Stage: "connect", Status: "done"})
				tracker.Update(progress.Event{Stage: "upload", Status: "started", Detail: sizeMB})
			},
			OnUploadProgress: func(sent, total int64) {
				pct := sent * 100 / total
				tracker.Update(progress.Event{
					Stage:  "upload",
					Status: "progress",
					Detail: fmt.Sprintf("%d%%  %s", pct, sizeMB),
				})
			},
		})

		var result *modal.TranscribeResult
		for evt := range events {
			switch evt.Type {
			case "init":
				// Upload is done — server received data and started processing
				tracker.Update(progress.Event{Stage: "upload", Status: "done", Detail: sizeMB})
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
							if err := httpClient.SetSpeakerName(ctx, result.AudioID, sid, n); err != nil {
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
