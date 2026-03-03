package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all recordings",
	RunE: func(cmd *cobra.Command, args []string) error {
		recordings, err := client.ListRecordings(cmd.Context())
		if err != nil {
			return err
		}

		if jsonOut {
			data, _ := json.MarshalIndent(recordings, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(recordings) == 0 {
			fmt.Println("No recordings found.")
			return nil
		}

		// Build a tag ID → name map
		tags, _ := client.ListTags(cmd.Context())
		tagMap := make(map[string]string)
		for _, t := range tags {
			tagMap[t.ID] = t.Name
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
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func init() {
	rootCmd.AddCommand(listCmd)
}
