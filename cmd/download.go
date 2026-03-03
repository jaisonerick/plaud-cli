package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jaisonerick/plaud-api/internal/api"
	"github.com/spf13/cobra"
)

var (
	dlAudio      bool
	dlTranscript bool
	dlSummary    bool
	dlFormat     string
	dlOutputDir  string
)

var downloadCmd = &cobra.Command{
	Use:   "download <id>",
	Short: "Download recording audio, transcript, or summary",
	Long: `Download recording files. With no flags, downloads audio only.

Examples:
  plaud download abc123                              # Audio (MP3)
  plaud download abc123 --transcript                 # Transcript as TXT
  plaud download abc123 --transcript --format srt    # Transcript as SRT
  plaud download abc123 --summary --format md        # Summary as Markdown
  plaud download abc123 --audio --transcript --summary --output-dir ./out`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]

		// Default to audio if no flags specified
		if !dlAudio && !dlTranscript && !dlSummary {
			dlAudio = true
		}

		// Fetch recording details for naming
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		baseName := sanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatDate(detail.CreatedAt), " ", "_")

		if dlAudio {
			fmt.Print("Downloading audio... ")
			tempURL, err := client.GetTempURL(ctx, id)
			if err != nil {
				return fmt.Errorf("getting download URL: %w", err)
			}

			ext := ".mp3"
			if detail.FileType != "" {
				ext = "." + strings.ToLower(detail.FileType)
			}
			dest := filepath.Join(dlOutputDir, baseName+ext)

			if err := client.DownloadFile(ctx, tempURL, dest); err != nil {
				return fmt.Errorf("downloading audio: %w", err)
			}
			fmt.Printf("saved to %s\n", dest)
		}

		if dlTranscript {
			if err := downloadDocument(ctx, detail, "transcript"); err != nil {
				return err
			}
		}

		if dlSummary {
			if err := downloadDocument(ctx, detail, "summary"); err != nil {
				return err
			}
		}

		return nil
	},
}

func downloadDocument(ctx context.Context, detail *api.RecordingDetail, docType string) error {
	format := dlFormat
	if format == "" {
		format = "txt"
	}

	baseName := sanitizeFilename(detail.Name) + "_" + strings.ReplaceAll(api.FormatDate(detail.CreatedAt), " ", "_")

	fmt.Printf("Exporting %s (%s)... ", docType, format)
	url, err := client.ExportDocument(ctx, detail.ID, docType, format)
	if err != nil {
		return fmt.Errorf("exporting %s: %w", docType, err)
	}

	dest := filepath.Join(dlOutputDir, baseName+"_"+docType+"."+format)
	if err := client.DownloadFile(ctx, url, dest); err != nil {
		return fmt.Errorf("downloading %s: %w", docType, err)
	}
	fmt.Printf("saved to %s\n", dest)
	return nil
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
	downloadCmd.Flags().StringVar(&dlFormat, "format", "txt", "export format: txt, srt, md, docx, pdf")
	downloadCmd.Flags().StringVar(&dlOutputDir, "output-dir", ".", "output directory")
	rootCmd.AddCommand(downloadCmd)
}
