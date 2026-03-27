package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	trOutputDir  string
	trFormat     string
	trOptions    string
	trContext    string
	trCompactGap int
	trLanguage string
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
		diarize, polish, compact := true, true, true
		if trOptions != "" {
			for _, opt := range strings.Split(trOptions, ",") {
				switch strings.TrimSpace(opt) {
				case "no-diarize":
					diarize = false
				case "no-polish":
					polish = false
				case "no-compact":
					compact = false
				default:
					return fmt.Errorf("unknown option %q (valid: no-diarize, no-polish, no-compact)", opt)
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

		segments, err := modal.Transcribe(ctx, modalCfg, audioData, modal.TranscribeOpts{
			Diarize:    diarize,
			Polish:     polish,
			Compact:    compact,
			CompactGap: trCompactGap,
			Language: trLanguage,
			ContextDoc: contextDoc,
		})
		if err != nil {
			return fmt.Errorf("transcribing: %w", err)
		}
		fmt.Fprintf(os.Stderr, "done (%d segments)\n", len(segments))

		// Save result
		var dest string
		if trFormat == "json" {
			dest = filepath.Join(trOutputDir, baseName+"_whisper.json")
			data, err := json.MarshalIndent(segments, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling transcript: %w", err)
			}
			if err := os.WriteFile(dest, data, 0644); err != nil {
				return fmt.Errorf("writing transcript: %w", err)
			}
		} else {
			ext, content := transcript.Format(segments, trFormat)
			dest = filepath.Join(trOutputDir, baseName+"_whisper"+ext)
			if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing transcript: %w", err)
			}
		}

		fmt.Printf("Transcript saved to %s\n", dest)
		return nil
	},
}

func init() {
	transcribeCmd.Flags().StringVar(&trOutputDir, "output-dir", ".", "output directory")
	transcribeCmd.Flags().StringVar(&trFormat, "format", "md", "output format: json, txt, srt, md")
	transcribeCmd.Flags().StringVar(&trOptions, "options", "", "comma-separated disable flags: no-diarize, no-polish, no-compact")
	transcribeCmd.Flags().StringVar(&trContext, "context", "", "path to meeting context file (agenda, notes) for better hotwords and polishing")
	transcribeCmd.Flags().IntVar(&trCompactGap, "compact-gap", 2000, "max silence gap in ms before starting a new paragraph")
	transcribeCmd.Flags().StringVar(&trLanguage, "language", "", "force language code (e.g. pt, en), empty for auto-detect")
	rootCmd.AddCommand(transcribeCmd)
}
