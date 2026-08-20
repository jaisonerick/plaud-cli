package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	dlFilter    recordingFilter
	dlAll       bool
	dlForce     bool
	dlAudio     bool
	dlSummary   bool
	dlOutputDir string
)

var downloadCmd = &cobra.Command{
	Use:   "download [id]",
	Short: "Fetch the audio or the summary of a recording, or of many",
	Long: `Fetch what Plaud already holds. With no flags, the audio.

The text of a recording is 'plaud transcript', which makes one when nobody
has yet; this only ever copies a file that exists.

Naming a recording does one. A filter, or --all, does every recording it keeps,
skipping the files already here unless --force says otherwise.

Examples:
  plaud download abc123
  plaud download abc123 --summary
  plaud download --since 2026-08-01 --audio --output-dir ./recordings
  plaud download --tag cliente --audio --summary`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if !dlAudio && !dlSummary {
			dlAudio = true
		}
		if err := os.MkdirAll(dlOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}

		recordings, err := chooseRecordings(ctx, args, dlFilter, dlAll)
		if err != nil {
			return err
		}
		if len(recordings) == 0 {
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		}

		var failed int
		for _, r := range recordings {
			if err := fetchFiles(ctx, r); err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", r.Name, err)
				failed++
			}
		}

		if len(recordings) > 1 {
			fmt.Fprintf(os.Stderr, "\n%d recording(s), %d failed\n", len(recordings), failed)
		}
		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

func fetchFiles(ctx context.Context, r api.RecordingSimple) error {
	base := filepath.Join(dlOutputDir, transcript.BaseName(r.Name, r.StartTime))

	if dlAudio {
		dest := base + ".mp3"
		if wanted(dest) {
			url, err := client.GetTempURL(ctx, r.ID)
			if err != nil {
				return fmt.Errorf("getting download URL: %w", err)
			}
			if err := client.DownloadFile(ctx, url, dest); err != nil {
				return fmt.Errorf("downloading audio: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Saved to %s\n", dest)
		}
	}

	if dlSummary {
		dest := base + "_summary.md"
		if !wanted(dest) {
			return nil
		}
		detail, err := client.GetDetail(ctx, r.ID)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}
		url := detail.SummaryURL()
		if url == "" {
			fmt.Fprintf(os.Stderr, "No summary for %s\n", r.Name)
			return nil
		}
		if err := client.DownloadGzipped(ctx, url, dest); err != nil {
			return fmt.Errorf("downloading summary: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Saved to %s\n", dest)
	}
	return nil
}

// wanted reports whether a file still has to be fetched. The directory is the
// record of what has been done, so there is no state file to go stale.
func wanted(dest string) bool {
	if dlForce {
		return true
	}
	_, err := os.Stat(dest)
	return err != nil
}

func init() {
	f := downloadCmd.Flags()
	addFilterFlags(downloadCmd, &dlFilter)
	f.BoolVar(&dlAll, "all", false, "every recording in the account")
	f.BoolVar(&dlForce, "force", false, "fetch again over a file already on disk")
	f.BoolVar(&dlAudio, "audio", false, "the audio file")
	f.BoolVar(&dlSummary, "summary", false, "the summary Plaud wrote")
	f.StringVar(&dlOutputDir, "output-dir", ".", "where the files are written")
	rootCmd.AddCommand(downloadCmd)
}
