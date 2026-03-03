package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	dlAudio      bool
	dlTranscript bool
	dlSummary    bool
	dlOutputDir  string
)

var downloadCmd = &cobra.Command{
	Use:   "download <id>",
	Short: "Download recording audio, transcript, or summary",
	Long: `Download recording files. With no flags, downloads audio only.

Examples:
  plaud download abc123                              # Audio (MP3)
  plaud download abc123 --transcript                 # Transcript
  plaud download abc123 --summary                    # Summary (Markdown)
  plaud download abc123 --audio --transcript --summary --output-dir ./out`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]

		// Default to audio if no flags specified
		if !dlAudio && !dlTranscript && !dlSummary {
			dlAudio = true
		}

		// Fetch recording details for naming and content URLs
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		baseName := sanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatEpochMs(detail.StartTime), " ", "_")

		if dlAudio {
			fmt.Print("Downloading audio... ")
			tempURL, err := client.GetTempURL(ctx, id)
			if err != nil {
				return fmt.Errorf("getting download URL: %w", err)
			}

			dest := filepath.Join(dlOutputDir, baseName+".mp3")
			if err := client.DownloadFile(ctx, tempURL, dest); err != nil {
				return fmt.Errorf("downloading audio: %w", err)
			}
			fmt.Printf("saved to %s\n", dest)
		}

		if dlTranscript {
			url := detail.TranscriptURL()
			if url == "" {
				fmt.Println("No transcript available for this recording.")
			} else {
				fmt.Print("Downloading transcript... ")
				dest := filepath.Join(dlOutputDir, baseName+"_transcript.json")
				if err := client.DownloadGzipped(ctx, url, dest); err != nil {
					return fmt.Errorf("downloading transcript: %w", err)
				}
				fmt.Printf("saved to %s\n", dest)
			}
		}

		if dlSummary {
			url := detail.SummaryURL()
			if url == "" {
				fmt.Println("No summary available for this recording.")
			} else {
				fmt.Print("Downloading summary... ")
				dest := filepath.Join(dlOutputDir, baseName+"_summary.md")
				if err := client.DownloadGzipped(ctx, url, dest); err != nil {
					return fmt.Errorf("downloading summary: %w", err)
				}
				fmt.Printf("saved to %s\n", dest)
			}
		}

		return nil
	},
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	s := replacer.Replace(name)
	s = strings.TrimSpace(s)
	if s == "" {
		s = "recording"
	}
	return s
}

func init() {
	downloadCmd.Flags().BoolVar(&dlAudio, "audio", false, "download audio file")
	downloadCmd.Flags().BoolVar(&dlTranscript, "transcript", false, "download transcript")
	downloadCmd.Flags().BoolVar(&dlSummary, "summary", false, "download summary")
	downloadCmd.Flags().StringVar(&dlOutputDir, "output-dir", ".", "output directory")
	rootCmd.AddCommand(downloadCmd)
}
