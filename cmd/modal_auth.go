package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var modalAuthCmd = &cobra.Command{
	Use:   "modal-auth",
	Short: "Configure Modal credentials for transcription",
	Long: `Save Modal API credentials for the transcribe command.

Get your token at https://modal.com/settings#tokens

Examples:
  plaud modal-auth
  plaud modal-auth --token-id ak-xxx --token-secret as-xxx`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenID, _ := cmd.Flags().GetString("token-id")
		tokenSecret, _ := cmd.Flags().GetString("token-secret")

		if tokenID == "" || tokenSecret == "" {
			reader := bufio.NewReader(os.Stdin)

			if tokenID == "" {
				fmt.Print("Modal Token ID: ")
				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading token ID: %w", err)
				}
				tokenID = strings.TrimSpace(input)
			}

			if tokenSecret == "" {
				fmt.Print("Modal Token Secret: ")
				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading token secret: %w", err)
				}
				tokenSecret = strings.TrimSpace(input)
			}
		}

		if tokenID == "" || tokenSecret == "" {
			return fmt.Errorf("both token ID and token secret are required")
		}

		cfg.ModalTokenID = tokenID
		cfg.ModalTokenSecret = tokenSecret

		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Println("Modal credentials saved.")
		return nil
	},
}

func init() {
	modalAuthCmd.Flags().String("token-id", "", "Modal token ID (ak-...)")
	modalAuthCmd.Flags().String("token-secret", "", "Modal token secret (as-...)")
	rootCmd.AddCommand(modalAuthCmd)
}
