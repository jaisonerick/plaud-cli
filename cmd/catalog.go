package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jaisonerick/plaud-cli/internal/catalog"
	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/spf13/cobra"
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Keep track of the recordings this repository knows about",
	Long: `A repository that declares "hub" in ` + repo.FileName + ` keeps a catalog: what
each recording is, whether it has been transcribed, and where it was filed.

The catalog is ` + catalog.FileName + ` in that directory, git-tracked, one JSON object
per recording, and it is the whole of what is stored. Half of each entry is
what Plaud says and is replaced on every refresh; the other half is what a
person decided, and a refresh never touches it.`,
}

// hub is the catalog of the repository this command runs in, or the reason
// there is none.
func hub() (*repo.Config, *catalog.Catalog, error) {
	r, err := repository()
	if err != nil {
		return nil, nil, err
	}
	if !r.KeepsCatalog() {
		return nil, nil, fmt.Errorf(`%s declares no "hub", so this repository keeps no catalog`, repo.FileName)
	}
	c, err := catalog.Open(r.Hub)
	return r, c, err
}

var catalogRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Merge the account's recordings into the catalog, keeping the curation",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		r, c, err := hub()
		if err != nil {
			return err
		}

		byID, _ := tagNames(ctx)
		recordings, err := client.ListRecordings(ctx)
		if err != nil {
			return err
		}

		excluded := map[string]bool{}
		for _, name := range r.ExcludeTags {
			excluded[strings.ToLower(name)] = true
		}

		var added, updated int
		for _, rec := range recordings {
			var tags []string
			outOfScope := false
			for _, id := range rec.Tags {
				name := byID[id]
				if name == "" {
					continue
				}
				tags = append(tags, name)
				if excluded[strings.ToLower(name)] {
					outOfScope = true
				}
			}
			sort.Strings(tags)

			entry, known := c.Get(rec.ID)
			if !known {
				entry = &catalog.Entry{ID: rec.ID}
				c.Put(entry)
				added++
			} else {
				updated++
			}

			entry.Filename = rec.Name
			entry.StartTime = rec.StartTime
			entry.RecordedAt = r.LocalTime(rec.StartTime).Format("2006-01-02 15:04:05")
			entry.DurationMS = rec.Duration
			entry.DurationMin = math.Round(float64(rec.Duration)/600) / 100
			entry.Scene = rec.Scene
			entry.URL = "https://web.plaud.ai/file/" + rec.ID
			entry.IsTrans = rec.HasTranscript
			entry.IsSummary = rec.HasSummary
			entry.Tags = tags

			if !entry.Recomputed() {
				continue
			}
			switch {
			case outOfScope:
				entry.Status = catalog.Excluded
				entry.ExcludedReason = catalog.Text(r.ExcludeReason)
			case entry.TranscriptPath != nil || rec.HasTranscript:
				entry.Status = catalog.Transcribed
			default:
				entry.Status = catalog.Pending
			}
		}

		if err := c.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%d recording(s): %d new, %d already known\n", c.Len(), added, updated)
		return nil
	},
}

var catalogStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Count the catalog by status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, c, err := hub()
		if err != nil {
			return err
		}

		counts := map[string]int{}
		for _, e := range c.Entries() {
			counts[e.Status]++
		}
		if jsonOut {
			out, _ := json.MarshalIndent(counts, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		names := make([]string, 0, len(counts))
		for name := range counts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%-14s %d\n", name, counts[name])
		}
		fmt.Printf("%-14s %d\n", "total", c.Len())
		return nil
	},
}

var (
	catStatus  string
	catProject string
	catTag     string
	catMinutes float64
	catUnfiled bool
	catLimit   int
)

var catalogListCmd = &cobra.Command{
	Use:   "list",
	Short: "The catalog, narrowed",
	Long: `Print catalog entries, narrowed by what a person actually asks about.

--json prints the entries whole, which is what to pipe somewhere when the
flags here do not reach the question.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, c, err := hub()
		if err != nil {
			return err
		}

		var kept []*catalog.Entry
		for _, e := range c.Entries() {
			if catStatus != "" && e.Status != catStatus {
				continue
			}
			if catProject != "" && catalog.Read(e.Project) != catProject {
				continue
			}
			if catTag != "" && !hasName(e.Tags, catTag) {
				continue
			}
			if e.DurationMin < catMinutes {
				continue
			}
			if catUnfiled && (e.Status == catalog.Filed || e.Status == catalog.Excluded) {
				continue
			}
			kept = append(kept, e)
			if catLimit > 0 && len(kept) >= catLimit {
				break
			}
		}

		if jsonOut {
			out, err := json.MarshalIndent(kept, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, e := range kept {
			fmt.Fprintf(w, "%s\t%s\t%.0fm\t%s\t%s\n", e.ID, e.RecordedAt, e.DurationMin, e.Status, e.Filename)
		}
		return w.Flush()
	},
}

var catalogSetCmd = &cobra.Command{
	Use:   "set <id> key=value [key=value ...]",
	Short: "Record what a person decided about one recording",
	Long: `Set the fields a refresh never touches: project, path, repo, status, notes,
transcript_path, summary_path.

Setting status to "filed" or "excluded" is what stops a refresh recomputing it.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, c, err := hub()
		if err != nil {
			return err
		}
		entry, known := c.Get(args[0])
		if !known {
			return fmt.Errorf("the catalog holds no recording %s — run 'plaud catalog refresh'", args[0])
		}

		for _, pair := range args[1:] {
			key, value, found := strings.Cut(pair, "=")
			if !found {
				return fmt.Errorf("%q is not key=value", pair)
			}
			if err := assign(entry, key, value); err != nil {
				return err
			}
		}
		return c.Save()
	},
}

func assign(e *catalog.Entry, key, value string) error {
	switch key {
	case "project":
		e.Project = catalog.Text(value)
	case "path":
		e.Path = catalog.Text(value)
	case "repo":
		e.Repo = catalog.Text(value)
	case "notes":
		e.Notes = catalog.Text(value)
	case "transcript_path":
		e.TranscriptPath = catalog.Text(value)
	case "summary_path":
		e.SummaryPath = catalog.Text(value)
	case "excluded_reason":
		e.ExcludedReason = catalog.Text(value)
	case "status":
		switch value {
		case catalog.Pending, catalog.Transcribed, catalog.Filed, catalog.Excluded:
			e.Status = value
		default:
			return fmt.Errorf("%q is not a status (pending, transcribed, filed, excluded)", value)
		}
	default:
		return fmt.Errorf("%q is not a field a person fills in", key)
	}
	return nil
}

func hasName(names []string, want string) bool {
	for _, name := range names {
		if strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}

func init() {
	f := catalogListCmd.Flags()
	f.StringVar(&catStatus, "status", "", "only entries with this status")
	f.StringVar(&catProject, "project", "", "only entries curated into this project")
	f.StringVar(&catTag, "tag", "", "only entries carrying this Plaud tag")
	f.Float64Var(&catMinutes, "min-minutes", 0, "skip recordings shorter than this")
	f.BoolVar(&catUnfiled, "unfiled", false, "only what nobody has filed or ruled out")
	f.IntVar(&catLimit, "limit", 0, "stop after this many")

	catalogCmd.AddCommand(catalogRefreshCmd, catalogStatusCmd, catalogListCmd, catalogSetCmd)
	rootCmd.AddCommand(catalogCmd)
}
