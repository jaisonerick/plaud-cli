package cmd

import (
	"fmt"

	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/spf13/cobra"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage known speaker identities",
}

var speakerSetCmd = &cobra.Command{
	Use:   "set <audio-id> <speaker-id> <name>",
	Short: "Register a speaker embedding under a name",
	Long: `Associates a speaker from a previous transcription with a name.
The speaker embedding is stored server-side and used for future recognition.

Example:
  plaud speaker set abc-123-def SPEAKER_00 "Alice"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		audioID := args[0]
		speakerID := args[1]
		name := args[2]

		httpClient := modal.LoadHTTPClient(cfg.ModalTokenID, cfg.ModalTokenSecret, cfg.ModalEndpointURL)
		if httpClient == nil {
			return fmt.Errorf("Modal not configured. Run 'plaud modal-auth' or set MODAL_TOKEN_ID, MODAL_TOKEN_SECRET, and MODAL_ENDPOINT_URL environment variables")
		}

		if err := httpClient.SetSpeakerName(ctx, audioID, speakerID, name); err != nil {
			return fmt.Errorf("setting speaker name: %w", err)
		}

		fmt.Printf("Speaker %q registered from audio %s/%s\n", name, audioID, speakerID)
		return nil
	},
}

var speakerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all known speakers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		httpClient := modal.LoadHTTPClient(cfg.ModalTokenID, cfg.ModalTokenSecret, cfg.ModalEndpointURL)
		if httpClient == nil {
			return fmt.Errorf("Modal not configured. Run 'plaud modal-auth' or set MODAL_TOKEN_ID, MODAL_TOKEN_SECRET, and MODAL_ENDPOINT_URL environment variables")
		}

		speakers, err := httpClient.ListKnownSpeakers(ctx)
		if err != nil {
			return fmt.Errorf("listing speakers: %w", err)
		}

		if len(speakers) == 0 {
			fmt.Println("No known speakers.")
			return nil
		}

		fmt.Println("Known speakers:")
		for _, s := range speakers {
			fmt.Printf("  %s\n", s)
		}
		return nil
	},
}

func init() {
	speakerCmd.AddCommand(speakerSetCmd)
	speakerCmd.AddCommand(speakerListCmd)
	rootCmd.AddCommand(speakerCmd)
}
