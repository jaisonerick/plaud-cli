package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	audioFilter    recordingFilter
	audioAll       bool
	audioForce     bool
	audioOutputDir string
)

var audioCmd = &cobra.Command{
	Use:   "audio [id]",
	Short: "Download the audio of a recording, or of many",
	Long: `Put a recording's audio file on disk.

The text of a recording is 'plaud fetch', which transcribes it when nobody has
and brings the summary along; this is only the audio, for the cases where the
sound itself is what is wanted.

Naming a recording does one. A filter, or --all, does every recording it keeps,
skipping the files already here unless --force says otherwise.

Examples:
  plaud audio abc123
  plaud audio --since 2026-08-01 --output-dir ./recordings`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if err := os.MkdirAll(audioOutputDir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
		recordings, err := chooseRecordings(ctx, args, audioFilter, audioAll)
		if err != nil {
			return err
		}
		if len(recordings) == 0 {
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		}

		var failed int
		for _, r := range recordings {
			dest := filepath.Join(audioOutputDir, transcript.BaseName(r.Name, r.StartTime)+".mp3")
			// The directory is the record of what has been done, so there is
			// no state file to go stale.
			if _, err := os.Stat(dest); err == nil && !audioForce {
				continue
			}
			url, err := client.GetTempURL(ctx, r.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: getting download URL: %v\n", r.Name, err)
				failed++
				continue
			}
			if err := client.DownloadFile(ctx, url, dest); err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", r.Name, err)
				failed++
				continue
			}
			fmt.Fprintf(os.Stderr, "Saved to %s\n", dest)
		}

		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

func init() {
	f := audioCmd.Flags()
	addFilterFlags(audioCmd, &audioFilter)
	f.BoolVar(&audioAll, "all", false, "every recording in the account")
	f.BoolVar(&audioForce, "force", false, "download again over a file already on disk")
	f.StringVar(&audioOutputDir, "output-dir", ".", "where the files are written")
	rootCmd.AddCommand(audioCmd)
}
