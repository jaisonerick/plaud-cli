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
	syncOnlyNew     bool
	syncDryRun      bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Bring in every recording this repository is missing",
	Long: `Fetch the recordings this repository takes in and does not have yet.

A profile names both where the recordings are filed, which the repository
declares, and the tag that selects them, which is yours: a tag lives in one
person's Plaud account. 'plaud profile set' writes your half. Filters work too, and
stand in for a profile in a repository that declares none.

What says a recording is already here is the file: the destination is worked
out from the same rules that wrote it, so running this twice decodes nothing
the second time.

A transcript already here is not left alone, though. Who a voice belongs to is
settled by the people known today, and somebody named since the file was
written is still SPEAKER_02 in it. Every transcript this brings in range of is
asked about again, and the ones that changed name are named in the output.
--only-new skips that.

Recordings carrying an excluded tag are left alone, and so are the ones turned
down with 'plaud triage skip'.

Examples:
  plaud sync --profile cerc
  plaud sync --profile cerc --dry-run
  plaud sync --tag CERC --since 2026-08-01
  plaud sync --profile cerc --only-new`,
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
		// A profile whose tag nobody set selects every recording in the
		// account, which is the one way this split fails quietly.
		if syncProfile != "" && filter.tag == "" && !filter.selects() && !syncAll {
			return missingTag(e.repo, syncProfile)
		}
		if !syncAll && !filter.selects() {
			return fmt.Errorf("nothing narrows this: pass --profile, a filter such as --tag or --since, or --all")
		}

		recordings, err := chooseRecordings(ctx, nil, filter, syncAll)
		if err != nil {
			return err
		}
		if len(recordings) == 0 {
			fmt.Fprintln(os.Stderr, "No recordings matched.")
			return nil
		}
		work, err := e.plan(ctx, recordings)
		if err != nil {
			return err
		}
		if len(work.fetch)+len(work.settle) == 0 {
			fmt.Fprintf(os.Stderr, "Nothing to do: all %d recording(s) that matched are out of scope here.\n", len(recordings))
			return nil
		}
		if syncDryRun {
			return work.describe(e.repo)
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%d recording(s) to bring in, %d already here to settle the names in.\n",
			len(work.fetch), len(work.settle))

		var written, failed int
		var renamed []report
		bring := func(r api.RecordingSimple, announce bool) {
			if announce {
				fmt.Fprintf(os.Stderr, "\n%s\n", r.Name)
			}
			done, err := e.run(ctx, whisper, r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", r.Name, err)
				failed++
				return
			}
			if jsonOut {
				printReport(done)
			}
			if done.Written {
				written++
			}
			if done.Renamed > 0 {
				renamed = append(renamed, done)
			}
		}
		for _, r := range work.fetch {
			bring(r, true)
		}
		for _, r := range work.settle {
			bring(r, false)
		}

		fmt.Fprintf(os.Stderr, "\n%d brought in, %d failed\n", written, failed)
		if len(renamed) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d transcript(s) already here now name somebody they did not:\n", len(renamed))
			for _, done := range renamed {
				fmt.Fprintf(os.Stderr, "  %s  (%d turn(s))\n", e.repo.Rel(done.Path), done.Renamed)
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

// work is what a sync has ahead of it: the recordings to bring in, and the
// transcripts already here whose names are worth settling again.
type work struct {
	fetch  []api.RecordingSimple
	settle []api.RecordingSimple
	at     map[string]string
}

// plan sorts the recordings a filter kept into those two, and drops whatever
// a tag rules out of this repository whatever the filter said.
func (e *errand) plan(ctx context.Context, recordings []api.RecordingSimple) (work, error) {
	excluded := map[string]bool{}
	for _, name := range e.repo.ExcludeTags {
		excluded[strings.ToLower(name)] = true
	}
	byID, _ := tagNames(ctx)

	planned := work{at: map[string]string{}}
	for _, r := range recordings {
		if outOfScope(r, byID, excluded) {
			continue
		}
		// A recording turned down here stays turned down, whatever a filter
		// widened to. Offering it again is what triage exists to stop.
		if _, turned := e.repo.Skipped[r.ID]; turned {
			continue
		}
		dest, err := e.destination(r)
		if err != nil {
			return work{}, err
		}
		planned.at[r.ID] = dest

		if _, err := os.Stat(dest); err != nil {
			planned.fetch = append(planned.fetch, r)
			continue
		}
		if !syncOnlyNew {
			planned.settle = append(planned.settle, r)
		}
	}
	return planned, nil
}

// describe says what a run would do, and stops.
func (w work) describe(r *repo.Config) error {
	fmt.Fprintf(os.Stderr, "%d recording(s) would be brought in:\n", len(w.fetch))
	for _, rec := range w.fetch {
		fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", rec.ID, api.FormatEpochMs(rec.StartTime), rec.Name)
	}
	if len(w.settle) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d transcript(s) already here would have their names settled again:\n", len(w.settle))
		for _, rec := range w.settle {
			fmt.Fprintf(os.Stderr, "  %s\n", r.Rel(w.at[rec.ID]))
		}
	}
	return nil
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
	f.BoolVar(&syncOnlyNew, "only-new", false, "leave the transcripts already here alone")
	f.BoolVar(&syncDryRun, "dry-run", false, "say what would be brought in, and stop")
	rootCmd.AddCommand(syncCmd)
}
