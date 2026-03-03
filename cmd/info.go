package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <id>",
	Short: "Show recording details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		detail, err := client.GetDetail(cmd.Context(), args[0])
		if err != nil {
			return err
		}

		if jsonOut {
			data, _ := json.MarshalIndent(detail, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("ID:         %s\n", detail.ID)
		fmt.Printf("Name:       %s\n", detail.Name)
		fmt.Printf("Date:       %s\n", api.FormatEpochMs(detail.StartTime))
		fmt.Printf("Duration:   %s\n", api.FormatDurationMs(detail.Duration))

		if len(detail.Tags) > 0 {
			fmt.Printf("Tags:       %s\n", strings.Join(detail.Tags, ", "))
		}

		fmt.Printf("Transcript: %v\n", detail.HasTranscript())
		fmt.Printf("Summary:    %v\n", detail.HasSummary())

		for _, c := range detail.ContentList {
			if c.TaskStatus == 1 && c.DataTitle != "" {
				fmt.Printf("\n--- %s ---\n", c.DataTitle)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
