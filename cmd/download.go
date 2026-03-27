package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	dlAudio      bool
	dlTranscript bool
	dlSummary    bool
	dlAll        bool
	dlOutputDir  string
	dlFormat     string
)

var downloadCmd = &cobra.Command{
	Use:   "download <id>",
	Short: "Download recording audio, transcript, or summary",
	Long: `Download recording files. With no flags, downloads audio only.

Examples:
  plaud download abc123                                    # Audio (MP3)
  plaud download abc123 --transcript                       # Transcript (JSON)
  plaud download abc123 --transcript --format md           # Transcript (Markdown)
  plaud download abc123 --transcript --format srt          # Transcript (SRT)
  plaud download abc123 --all                              # Audio + transcript + summary
  plaud download abc123 --all --format txt                 # Audio + transcript (txt) + summary`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]

		// --all sets all three flags
		if dlAll {
			dlAudio = true
			dlTranscript = true
			dlSummary = true
		}

		// Default to audio if no flags specified
		if !dlAudio && !dlTranscript && !dlSummary {
			dlAudio = true
		}

		// Validate format
		switch dlFormat {
		case "json", "txt", "srt", "md":
			// ok
		default:
			return fmt.Errorf("unsupported format %q (use json, txt, srt, or md)", dlFormat)
		}

		// Ensure output directory exists
		if err := os.MkdirAll(dlOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		// Fetch recording details for naming and content URLs
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		baseName := transcript.SanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatEpochMs(detail.StartTime), " ", "_")

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
			} else if dlFormat == "json" {
				// Original behavior: save raw JSON
				fmt.Print("Downloading transcript... ")
				dest := filepath.Join(dlOutputDir, baseName+"_transcript.json")
				if err := client.DownloadGzipped(ctx, url, dest); err != nil {
					return fmt.Errorf("downloading transcript: %w", err)
				}
				fmt.Printf("saved to %s\n", dest)
			} else {
				// Download, parse, convert
				fmt.Printf("Downloading transcript (%s)... ", dlFormat)
				data, err := client.FetchGzipped(ctx, url)
				if err != nil {
					return fmt.Errorf("downloading transcript: %w", err)
				}

				segments, err := transcript.Parse(data)
				if err != nil {
					return fmt.Errorf("parsing transcript: %w", err)
				}

				ext, content := transcript.Format(segments, dlFormat)
				dest := filepath.Join(dlOutputDir, baseName+"_transcript"+ext)
				if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
					return fmt.Errorf("writing transcript: %w", err)
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


func init() {
	downloadCmd.Flags().BoolVar(&dlAudio, "audio", false, "download audio file")
	downloadCmd.Flags().BoolVar(&dlTranscript, "transcript", false, "download transcript")
	downloadCmd.Flags().BoolVar(&dlSummary, "summary", false, "download summary")
	downloadCmd.Flags().BoolVar(&dlAll, "all", false, "download audio, transcript, and summary")
	downloadCmd.Flags().StringVar(&dlOutputDir, "output-dir", ".", "output directory")
	downloadCmd.Flags().StringVar(&dlFormat, "format", "json", "transcript format: json, txt, srt, md")
	rootCmd.AddCommand(downloadCmd)
}
