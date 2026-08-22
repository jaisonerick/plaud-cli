package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	triageFilter      recordingFilter
	triageAll         bool
	triageContext     string
	triageContextFile string
	triageLanguage    string
	triageExcerpt     int
	triageReason      string
)

var triageCmd = &cobra.Command{
	Use:   "triage",
	Short: "Work out which of your recordings belong in this repository",
	Long: `Read what has not been decided yet, so somebody can say what belongs here.

A tag is the short way to say which recordings are this repository's, and
plenty of accounts have none. This is the other way: every recording that is
not already here and has not been turned down is transcribed and described —
who spoke, and enough of what was said to place it.

Nothing is written to disk. Transcribing is what makes a recording readable at
all, and the service keeps what it decoded, so a recording kept afterwards
comes back in seconds and one turned down cost a single pass, ever.

Then say what it is:

  plaud fetch <id>                     it belongs here
  plaud triage skip <id> --reason X    it does not, and stop offering it

Examples:
  plaud triage --since 2026-08-01
  plaud triage --limit 10 --json
  plaud triage --all --excerpt 1200`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		e, err := newErrand("", triageContext, triageContextFile, "md", triageLanguage, false)
		if err != nil {
			return err
		}
		if e.description == "" {
			return fmt.Errorf("%s", noDescription)
		}
		if !triageAll && !triageFilter.selects() {
			return fmt.Errorf("nothing narrows this: pass a filter such as --since or --limit, or --all")
		}

		recordings, err := chooseRecordings(ctx, nil, triageFilter, triageAll)
		if err != nil {
			return err
		}
		undecided, err := e.undecided(recordings)
		if err != nil {
			return err
		}
		if len(undecided) == 0 {
			fmt.Fprintf(os.Stderr, "Nothing to decide: all %d recording(s) that matched are either here or turned down.\n", len(recordings))
			return nil
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Reading %d recording(s). The ones nobody has transcribed take a pass each; the rest come back in seconds.\n", len(undecided))

		var failed int
		for _, r := range undecided {
			card, err := e.read(ctx, whisper, r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error on %s: %v\n", r.Name, err)
				failed++
				continue
			}
			show(card)
		}
		if failed > 0 {
			return fmt.Errorf("%d recording(s) failed", failed)
		}
		return nil
	},
}

// card is one recording as somebody deciding about it needs to see it. The
// speakers matter more than the words: a voice the service recognises says
// whose meeting this was, which the opening minute of arriving small talk does
// not.
type card struct {
	Recording  string   `json:"recording_id"`
	Name       string   `json:"name"`
	RecordedAt string   `json:"recorded_at"`
	Minutes    float64  `json:"duration_min"`
	Tags       []string `json:"tags,omitempty"`
	Speakers   []string `json:"speakers,omitempty"`
	Excerpt    string   `json:"excerpt"`
}

func show(c card) {
	if jsonOut {
		line, err := json.Marshal(c)
		if err != nil {
			return
		}
		fmt.Println(string(line))
		return
	}
	fmt.Printf("\n%s  %s  %.0f min\n", c.Recording, c.RecordedAt, c.Minutes)
	fmt.Printf("  %s\n", c.Name)
	if len(c.Tags) > 0 {
		fmt.Printf("  tags:     %s\n", strings.Join(c.Tags, ", "))
	}
	if len(c.Speakers) > 0 {
		fmt.Printf("  speakers: %s\n", strings.Join(c.Speakers, ", "))
	}
	fmt.Printf("  %s\n", c.Excerpt)
}

// undecided keeps what nobody has placed: not already at its destination here,
// and not turned down.
func (e *errand) undecided(recordings []api.RecordingSimple) ([]api.RecordingSimple, error) {
	var open []api.RecordingSimple
	for _, r := range recordings {
		if _, turned := e.repo.Skipped[r.ID]; turned {
			continue
		}
		dest, err := e.destination(r)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		open = append(open, r)
	}
	return open, nil
}

// read transcribes a recording and describes it, without writing anything. The
// service keeps the transcript, so this is what a recording costs once whether
// it is kept afterwards or not.
func (e *errand) read(ctx context.Context, whisper *modal.HTTPClient, r api.RecordingSimple) (card, error) {
	result, _, err := whisperTranscribe(ctx, os.Stderr, whisper, r.ID, e.how.opts)
	if err != nil {
		return card{}, err
	}

	c := card{
		Recording:  r.ID,
		Name:       r.Name,
		RecordedAt: e.repo.LocalTime(r.StartTime).Format("2006-01-02 15:04"),
		Minutes:    float64(r.Duration) / 60000,
		Excerpt:    excerpt(result.Segments, triageExcerpt),
	}
	for _, name := range result.Speakers {
		if !strings.HasPrefix(name, "SPEAKER_") {
			c.Speakers = append(c.Speakers, name)
		}
	}
	sort.Strings(c.Speakers)
	if len(result.Segments) == 0 {
		c.Excerpt = "(no speech)"
	}
	return c, nil
}

// excerpt is enough of what was said to place a recording, taken from the
// start because that is where a meeting says what it is for.
func excerpt(segments []transcript.Segment, chars int) string {
	var b strings.Builder
	for _, s := range segments {
		if b.Len() >= chars {
			break
		}
		b.WriteString(strings.TrimSpace(s.Content))
		b.WriteByte(' ')
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if len(text) > chars {
		text = strings.TrimSpace(text[:chars]) + "…"
	}
	return text
}

var triageSkipCmd = &cobra.Command{
	Use:   "skip <id> [<id> ...]",
	Short: "Turn a recording down for this repository, so it stops being offered",
	Long: `Record that a recording is not this repository's.

It is your decision about your own recordings, so it goes in your settings
rather than in the repository's file, and another person working here decides
for themselves.

The reason is worth writing: what tells a recording turned down for being
another client's from one turned down for being three seconds of pocket noise
is the reason, and six months later neither the date nor the title says.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		settings, err := repo.LoadSettings()
		if err != nil {
			return err
		}

		mine := settings.For(r.Identity, true)
		if mine.Skipped == nil {
			mine.Skipped = map[string]string{}
		}
		why := triageReason
		if why == "" {
			why = "not this repository"
		}
		for _, id := range args {
			mine.Skipped[id] = why
		}
		if err := settings.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Turned down %d recording(s) for %s\n", len(args), r.Identity)
		return nil
	},
}

var triageUnskipCmd = &cobra.Command{
	Use:   "unskip <id> [<id> ...]",
	Short: "Offer a recording again",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		settings, err := repo.LoadSettings()
		if err != nil {
			return err
		}

		mine := settings.For(r.Identity, false)
		if mine == nil || len(mine.Skipped) == 0 {
			return fmt.Errorf("you have turned nothing down for %s", r.Identity)
		}
		for _, id := range args {
			delete(mine.Skipped, id)
		}
		if err := settings.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Offering %d recording(s) again\n", len(args))
		return nil
	},
}

var triageSkippedCmd = &cobra.Command{
	Use:   "skipped",
	Short: "What you have turned down here, and why",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		if len(r.Skipped) == 0 {
			fmt.Fprintf(os.Stderr, "You have turned nothing down for %s.\n", r.Identity)
			return nil
		}
		if jsonOut {
			out, err := json.MarshalIndent(r.Skipped, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}

		ids := make([]string, 0, len(r.Skipped))
		for id := range r.Skipped {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("%s  %s\n", id, r.Skipped[id])
		}
		return nil
	},
}

func init() {
	f := triageCmd.Flags()
	addFilterFlags(triageCmd, &triageFilter)
	f.BoolVar(&triageAll, "all", false, "every recording in the account")
	f.StringVar(&triageContext, "context", "", "what this work is about, added to the repository's document")
	f.StringVar(&triageContextFile, "context-file", "", "a file describing it, standing in for the repository's document")
	f.StringVar(&triageLanguage, "language", "", "settle the language (e.g. pt)")
	f.IntVar(&triageExcerpt, "excerpt", 600, "how many characters of speech to show per recording")

	triageSkipCmd.Flags().StringVar(&triageReason, "reason", "", "why it is not this repository's")

	triageCmd.AddCommand(triageSkipCmd, triageUnskipCmd, triageSkippedCmd)
	rootCmd.AddCommand(triageCmd)
}
