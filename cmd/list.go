package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	listFilter recordingFilter
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

		tagMap, tagNameToID := tagNames(cmd.Context())
		recordings = listFilter.apply(recordings, tagNameToID)

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

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func init() {
	addFilterFlags(listCmd, &listFilter)
	rootCmd.AddCommand(listCmd)
}
