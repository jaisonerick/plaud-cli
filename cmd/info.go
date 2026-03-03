package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jaisonerick/plaud-api/internal/api"
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
		fmt.Printf("Date:       %s\n", api.FormatDate(detail.CreatedAt))
		fmt.Printf("Duration:   %s\n", api.FormatDuration(detail.Duration))
		fmt.Printf("Type:       %s\n", detail.FileType)

		if len(detail.Tags) > 0 {
			names := make([]string, len(detail.Tags))
			for i, t := range detail.Tags {
				names[i] = t.Name
			}
			fmt.Printf("Tags:       %s\n", strings.Join(names, ", "))
		}

		fmt.Printf("Transcript: %v\n", detail.HasTranscript)
		fmt.Printf("Summary:    %v\n", detail.HasSummary)

		if detail.Transcript != "" {
			fmt.Printf("\n--- Transcript ---\n%s\n", detail.Transcript)
		}

		if detail.Summary != "" {
			fmt.Printf("\n--- Summary ---\n%s\n", detail.Summary)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
