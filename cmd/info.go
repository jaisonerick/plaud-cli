package cmd

import (
	"context"
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

		fmt.Printf("Transcript on Plaud: %s\n", yesNo(detail.HasTranscript()))
		fmt.Printf("Transcribed here:    %s\n", transcribedHere(cmd.Context(), args[0]))
		fmt.Printf("Summary:    %v\n", detail.HasSummary())

		for _, c := range detail.ContentList {
			if c.TaskStatus == 1 && c.DataTitle != "" {
				fmt.Printf("\n--- %s ---\n", c.DataTitle)
			}
		}

		return nil
	},
}

func yesNo(is bool) string {
	if is {
		return "yes"
	}
	return "no"
}

// transcribedHere answers from the voices the transcription service kept. A
// transcript made here never reaches Plaud, so Plaud's own record says no
// however many times a recording has been through this.
func transcribedHere(ctx context.Context, recordingID string) string {
	whisper, err := whisperClient()
	if err != nil {
		return "unknown — not signed in to the transcription service"
	}
	voices, err := whisper.RecordingVoices(ctx, recordingID)
	if err != nil {
		return "unknown — the transcription service did not answer"
	}
	if len(voices) == 0 {
		return "no"
	}
	return fmt.Sprintf("yes, %d voice(s) on file", len(voices))
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
