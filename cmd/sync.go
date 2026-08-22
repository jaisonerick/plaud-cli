package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/spf13/cobra"
)

var (
	syncProfile     string
	syncFilter      recordingFilter
	syncAll         bool
	syncContext     string
	syncContextFile string
	syncLanguage    string
	syncFormat      string
	syncRefresh     bool
	syncDryRun      bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Bring in every recording this repository is missing",
	Long: `Fetch the recordings this repository takes in and does not have yet.

A profile names both the tag that selects recordings and where they are filed,
so 'plaud sync --profile cerc' is the whole instruction. Filters work too, and
stand in for a profile in a repository that declares none.

What says a recording is already here is the file: the destination is worked
out from the same rules that wrote it, so running this twice fetches nothing
the second time. --refresh also settles the names in the files already here,
which costs a request each and is worth it after naming somebody.

Recordings carrying an excluded tag are left alone.

Examples:
  plaud sync --profile cerc
  plaud sync --profile cerc --dry-run
  plaud sync --tag CERC --since 2026-08-01
  plaud sync --profile cerc --refresh`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		e, err := newErrand(syncProfile, syncContext, syncContextFile, syncFormat, syncLanguage, false)
		if err != nil {
			return err
		}

		filter := syncFilter
		if filter.tag == "" && e.profile.Tag != "" {
			filter.tag = e.profile.Tag
		}
		if !syncAll && !filter.selects() {
			return fmt.Errorf("nothing narrows this: pass --profile, a filter such as --tag or --since, or --all")
		}

		recordings, err := chooseRecordings(ctx, nil, filter, syncAll)
		if err != nil {
			return err
		}
		wanted, err := e.missing(ctx, recordings)
		if err != nil {
			return err
		}

		switch {
		case len(recordings) == 0:
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		case len(wanted) == 0:
			fmt.Fprintf(os.Stderr, "Nothing to bring in: all %d recording(s) that matched are already here.\n", len(recordings))
			return nil
		}
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "%d of %d recording(s) would be brought in:\n", len(wanted), len(recordings))
			for _, r := range wanted {
				fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", r.ID, api.FormatEpochMs(r.StartTime), r.Name)
			}
			return nil
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Bringing in %d of %d recording(s).\n", len(wanted), len(recordings))
		var failed int
		for _, r := range wanted {
			fmt.Fprintf(os.Stderr, "\n%s\n", r.Name)
			if _, _, err := e.run(ctx, whisper, r); err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", r.Name, err)
				failed++
			}
		}
		fmt.Fprintf(os.Stderr, "\n%d brought in, %d failed\n", len(wanted)-failed, failed)
		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

// missing keeps the recordings this repository does not have, which is what
// makes running this twice fetch nothing the second time. A recording ruled
// out by tag is left alone whatever the filter said.
func (e *errand) missing(ctx context.Context, recordings []api.RecordingSimple) ([]api.RecordingSimple, error) {
	excluded := map[string]bool{}
	for _, name := range e.repo.ExcludeTags {
		excluded[strings.ToLower(name)] = true
	}
	byID, _ := tagNames(ctx)

	var wanted []api.RecordingSimple
	for _, r := range recordings {
		if outOfScope(r, byID, excluded) {
			continue
		}
		dest, err := e.destination(r)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(dest); err == nil && !syncRefresh {
			continue
		}
		wanted = append(wanted, r)
	}
	return wanted, nil
}

func outOfScope(r api.RecordingSimple, byID map[string]string, excluded map[string]bool) bool {
	for _, id := range r.Tags {
		if excluded[strings.ToLower(byID[id])] {
			return true
		}
	}
	return false
}

func init() {
	f := syncCmd.Flags()
	addFilterFlags(syncCmd, &syncFilter)
	f.StringVar(&syncProfile, "profile", "", "the set of rules in "+repo.FileName+" to sync")
	f.BoolVar(&syncAll, "all", false, "every recording in the account")
	f.StringVar(&syncContext, "context", "", "what these recordings are about, added to the repository's document")
	f.StringVar(&syncContextFile, "context-file", "", "a file describing them, standing in for the repository's document")
	f.StringVar(&syncLanguage, "language", "", "settle the language (e.g. pt)")
	f.StringVar(&syncFormat, "format", "md", "output format: json, txt, srt, md")
	f.BoolVar(&syncRefresh, "refresh", false, "also settle the names in the transcripts already here")
	f.BoolVar(&syncDryRun, "dry-run", false, "say what would be brought in, and stop")
	rootCmd.AddCommand(syncCmd)
}
