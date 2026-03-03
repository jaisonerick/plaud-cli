package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"github.com/spf13/cobra"
)

var opRef string

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Plaud.ai",
	Long: `Authenticate with Plaud.ai. Three modes:

  plaud login                                    # Interactive prompt
  plaud login --op op://Personal/Plaud           # 1Password
  PLAUD_USERNAME=x PLAUD_PASSWORD=y plaud login  # Environment variables`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		username, password, err := getCredentials()
		if err != nil {
			return err
		}

		fmt.Print("Authenticating... ")
		token, err := client.Login(ctx, username, password)
		if err != nil {
			fmt.Println("failed.")
			return fmt.Errorf("login failed: %w", err)
		}
		fmt.Println("ok.")

		cfg.AccessToken = token
		cfg.BaseURL = client.BaseURL
		cfg.EnsureDeviceID()

		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Println("Token saved. You're logged in.")
		return nil
	},
}

func getCredentials() (string, string, error) {
	// Priority: env vars → --op flag → interactive
	if u, p := os.Getenv("PLAUD_USERNAME"), os.Getenv("PLAUD_PASSWORD"); u != "" && p != "" {
		return u, p, nil
	}

	if opRef != "" {
		return getCredentialsFromOP(opRef)
	}

	return getCredentialsInteractive()
}

func getCredentialsFromOP(ref string) (string, string, error) {
	username, err := opRead(ref + "/username")
	if err != nil {
		return "", "", fmt.Errorf("reading username from 1Password: %w", err)
	}

	password, err := opRead(ref + "/password")
	if err != nil {
		return "", "", fmt.Errorf("reading password from 1Password: %w", err)
	}

	return username, password, nil
}

func opRead(ref string) (string, error) {
	out, err := exec.Command("op", "read", ref).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getCredentialsInteractive() (string, string, error) {
	fmt.Print("Email: ")
	var email string
	if _, err := fmt.Scanln(&email); err != nil {
		return "", "", fmt.Errorf("reading email: %w", err)
	}

	fmt.Print("Password: ")
	passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", "", fmt.Errorf("reading password: %w", err)
	}

	return strings.TrimSpace(email), string(passBytes), nil
}

func init() {
	loginCmd.Flags().StringVar(&opRef, "op", "", "1Password item reference (e.g. op://Personal/Plaud)")
	rootCmd.AddCommand(loginCmd)
}
