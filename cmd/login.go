package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	tokenFlag         string
	emailFlag         string
	passwordFlag      bool
	passwordStdinFlag bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Plaud.ai",
	Long: `Authenticate with Plaud.ai via password, email code, or an existing token.

  plaud login --password                       # Email and password
  plaud login --email you@example.com --password
  plaud login --email you@example.com --password-stdin < secret
  plaud login                                  # Interactive email code flow
  plaud login --token TOKEN                    # Use an existing access token

The password is prompted for rather than taken as an argument, which would put
it in the shell history and in the process list. For automation, pass it on
stdin or in PLAUD_PASSWORD.

An account created through Google, Apple or Microsoft has no password until one
is set in the Plaud app; use the email code flow for those.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Direct token login (e.g. from browser session)
		if tokenFlag != "" {
			return saveToken(tokenFlag)
		}

		email := os.Getenv("PLAUD_EMAIL")
		if emailFlag != "" {
			email = emailFlag
		}
		code := os.Getenv("PLAUD_CODE")
		otpToken := os.Getenv("PLAUD_OTP_TOKEN")

		if passwordFlag || passwordStdinFlag || os.Getenv("PLAUD_PASSWORD") != "" {
			return passwordLogin(cmd, email)
		}

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

// passwordLogin authenticates with an email and password, the flow that needs
// neither a terminal nor access to the account's mailbox, and is therefore the
// one a new machine can be set up with in a single step.
func passwordLogin(cmd *cobra.Command, email string) error {
	if email == "" {
		fmt.Print("Email: ")
		if _, err := fmt.Scanln(&email); err != nil {
			return fmt.Errorf("reading email: %w", err)
		}
		email = strings.TrimSpace(email)
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("no password given")
	}

	fmt.Print("Authenticating... ")
	token, err := client.PasswordLogin(cmd.Context(), email, password)
	if err != nil {
		fmt.Println("failed.")
		return err
	}
	fmt.Println("ok.")
	return saveToken(token)
}

// readPassword takes the password from stdin or an environment variable when
// one is offered, and otherwise prompts for it without echoing.
func readPassword() (string, error) {
	if env := os.Getenv("PLAUD_PASSWORD"); env != "" {
		return env, nil
	}
	if passwordStdinFlag {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal to prompt on: pass the password with --password-stdin or PLAUD_PASSWORD")
	}
	fmt.Print("Password: ")
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(data), nil
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
	loginCmd.Flags().StringVar(&tokenFlag, "token", "", "use an existing access token (e.g. from browser DevTools)")
	loginCmd.Flags().StringVar(&emailFlag, "email", "", "account email (also read from PLAUD_EMAIL)")
	loginCmd.Flags().BoolVar(&passwordFlag, "password", false, "authenticate with a password, prompted for")
	loginCmd.Flags().BoolVar(&passwordStdinFlag, "password-stdin", false, "read the password from stdin")
	rootCmd.AddCommand(loginCmd)
}
