package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Plaud.ai using an email code",
	Long: `Authenticate with Plaud.ai via the "Sign in with a code" flow.

  plaud login                                    # Interactive prompt
  PLAUD_EMAIL=x PLAUD_CODE=y PLAUD_OTP_TOKEN=z plaud login  # Skip prompts`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		email := os.Getenv("PLAUD_EMAIL")
		code := os.Getenv("PLAUD_CODE")
		otpToken := os.Getenv("PLAUD_OTP_TOKEN")

		// If all env vars are set, skip prompts entirely
		if otpToken != "" && code != "" {
			fmt.Print("Authenticating... ")
			token, err := client.VerifyCode(ctx, otpToken, code)
			if err != nil {
				fmt.Println("failed.")
				return fmt.Errorf("login failed: %w", err)
			}
			fmt.Println("ok.")
			return saveToken(token)
		}

		// Step 1: get email
		if email == "" {
			fmt.Print("Email: ")
			if _, err := fmt.Scanln(&email); err != nil {
				return fmt.Errorf("reading email: %w", err)
			}
			email = strings.TrimSpace(email)
		}

		// Step 2: send code
		fmt.Printf("Sending code to %s... ", email)
		otp, err := client.SendCode(ctx, email)
		if err != nil {
			fmt.Println("failed.")
			return fmt.Errorf("sending code: %w", err)
		}
		fmt.Println("ok.")

		// Step 3: get code
		if code == "" {
			fmt.Print("Code: ")
			if _, err := fmt.Scanln(&code); err != nil {
				return fmt.Errorf("reading code: %w", err)
			}
			code = strings.TrimSpace(code)
		}

		// Step 4: verify
		fmt.Print("Authenticating... ")
		token, err := client.VerifyCode(ctx, otp, code)
		if err != nil {
			fmt.Println("failed.")
			return fmt.Errorf("login failed: %w", err)
		}
		fmt.Println("ok.")

		return saveToken(token)
	},
}

func saveToken(token string) error {
	cfg.AccessToken = token
	cfg.BaseURL = client.BaseURL
	cfg.EnsureDeviceID()

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("Token saved. You're logged in.")
	return nil
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
