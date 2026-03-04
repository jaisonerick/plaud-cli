package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	listTag           string
	listSince         string
	listBefore        string
	listHasTranscript bool
	listHasSummary    bool
	listLimit         int
	listSearch        string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all recordings",
	Long: `List recordings with optional filters.

Examples:
  plaud list
  plaud list --tag "Work" --has-transcript
  plaud list --since 2025-01-01 --limit 10
  plaud list --search "meeting"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		recordings, err := client.ListRecordings(cmd.Context())
		if err != nil {
			return err
		}

		// Build a tag ID → name map (and name → ID for filtering)
		tags, _ := client.ListTags(cmd.Context())
		tagMap := make(map[string]string)
		tagNameToID := make(map[string]string)
		for _, t := range tags {
			tagMap[t.ID] = t.Name
			tagNameToID[strings.ToLower(t.Name)] = t.ID
		}

		// Apply filters
		recordings = filterRecordings(recordings, tagMap, tagNameToID)

		if jsonOut {
			data, _ := json.MarshalIndent(recordings, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(recordings) == 0 {
			fmt.Println("No recordings found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tDATE\tDURATION\tNAME\tTAGS\tT\tS")
		for _, r := range recordings {
			tagNames := make([]string, 0, len(r.Tags))
			for _, tid := range r.Tags {
				if name, ok := tagMap[tid]; ok {
					tagNames = append(tagNames, name)
				}
			}

			transcript := "-"
			if r.HasTranscript {
				transcript = "Y"
			}
			summary := "-"
			if r.HasSummary {
				summary = "Y"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.ID,
				api.FormatEpochMs(r.StartTime),
				api.FormatDurationMs(r.Duration),
				truncate(r.Name, 40),
				strings.Join(tagNames, ", "),
				transcript,
				summary,
			)
		}
		w.Flush()

		fmt.Printf("\n%d recording(s)\n", len(recordings))
		return nil
	},
}

func filterRecordings(recordings []api.RecordingSimple, tagMap, tagNameToID map[string]string) []api.RecordingSimple {
	hasFilters := listTag != "" || listSince != "" || listBefore != "" ||
		listHasTranscript || listHasSummary || listSearch != "" || listLimit > 0

	if !hasFilters {
		return recordings
	}

	// Resolve tag filter to ID
	var filterTagID string
	if listTag != "" {
		if id, ok := tagNameToID[strings.ToLower(listTag)]; ok {
			filterTagID = id
		} else {
			// No matching tag — return empty
			return nil
		}
	}

	// Parse date filters
	var sinceTime, beforeTime time.Time
	if listSince != "" {
		sinceTime, _ = time.Parse("2006-01-02", listSince)
	}
	if listBefore != "" {
		beforeTime, _ = time.Parse("2006-01-02", listBefore)
	}

	searchLower := strings.ToLower(listSearch)

	var filtered []api.RecordingSimple
	for _, r := range recordings {
		// Tag filter
		if filterTagID != "" {
			found := false
			for _, tid := range r.Tags {
				if tid == filterTagID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Date filters (StartTime is epoch ms)
		recTime := time.Unix(0, r.StartTime*int64(time.Millisecond))
		if !sinceTime.IsZero() && recTime.Before(sinceTime) {
			continue
		}
		if !beforeTime.IsZero() && recTime.After(beforeTime.Add(24*time.Hour)) {
			continue
		}

		// Content filters
		if listHasTranscript && !r.HasTranscript {
			continue
		}
		if listHasSummary && !r.HasSummary {
			continue
		}

		// Name search
		if listSearch != "" && !strings.Contains(strings.ToLower(r.Name), searchLower) {
			continue
		}

		filtered = append(filtered, r)

		// Limit
		if listLimit > 0 && len(filtered) >= listLimit {
			break
		}
	}

	return filtered
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func init() {
	listCmd.Flags().StringVar(&listTag, "tag", "", "filter by tag name")
	listCmd.Flags().StringVar(&listSince, "since", "", "filter recordings after date (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&listBefore, "before", "", "filter recordings before date (YYYY-MM-DD)")
	listCmd.Flags().BoolVar(&listHasTranscript, "has-transcript", false, "only show recordings with transcripts")
	listCmd.Flags().BoolVar(&listHasSummary, "has-summary", false, "only show recordings with summaries")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "limit number of results")
	listCmd.Flags().StringVar(&listSearch, "search", "", "filter by recording name substring")
	rootCmd.AddCommand(listCmd)
}
