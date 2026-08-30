package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	tokenFlag    string
	emailFlag    string
	passwordFlag bool
	sendCodeFlag bool
	codeFlag     string
	otpTokenFlag string
	migrateFlag  bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Plaud.ai",
	Long: `Authenticate with Plaud.ai via an email code, a password, or an existing token.

  plaud login                                  # Interactive email code flow
  plaud login --password                       # Email and password
  plaud login --token TOKEN                    # Use an existing access token
  plaud login --migrate                        # Replace a pre-v3 bearer token with a session

The email code flow also comes in two halves, so that something other than this
terminal can collect the code: a chat with an assistant, a form, another
machine. Nothing is stored between the two calls except the handle printed by
the first:

  plaud login --send-code --email you@example.com [--json]
  plaud login --email you@example.com --otp-token TOKEN --code 123456

Prefer that over the password whenever a third party is doing the typing: the
code expires and is good once, a password is not. The password is prompted for
rather than taken as an argument, which would put it in the shell history and
in the process list; --password-stdin and PLAUD_PASSWORD cover automation.

An account created through Google, Apple or Microsoft has no password until one
is set in the Plaud app, so those accounts use the code flow.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// --migrate is a guard rather than a second way in. The migration is
		// the ordinary code login: saveSession already clears the bearer token
		// it replaces, so there is nothing here the flag has to do differently.
		// What it adds is refusing when there is nothing to replace, which is
		// what makes a reminder printed by another command safe to follow
		// without first working out whether it applies.
		if migrateFlag {
			if cfg.Scheme() == config.SessionScheme {
				fmt.Println("Already signed in with a v3 session. Nothing to migrate.")
				return nil
			}
			if tokenFlag != "" {
				return fmt.Errorf("--migrate replaces a bearer token with a session, and --token stores another one")
			}
		}

		// Direct token login (e.g. from browser session)
		if tokenFlag != "" {
			return saveToken(tokenFlag)
		}

		email := os.Getenv("PLAUD_EMAIL")
		if emailFlag != "" {
			email = emailFlag
		}
		code := os.Getenv("PLAUD_CODE")
		if codeFlag != "" {
			code = codeFlag
		}
		otpToken := os.Getenv("PLAUD_OTP_TOKEN")
		if otpTokenFlag != "" {
			otpToken = otpTokenFlag
		}

		if passwordFlag || os.Getenv("PLAUD_PASSWORD") != "" {
			return passwordLogin(cmd, email)
		}

		// First half of the split code flow: send the code and hand the caller
		// the handle to finish with, so the two steps can be minutes and one
		// conversation apart.
		if sendCodeFlag {
			if email == "" {
				return fmt.Errorf("--send-code needs --email")
			}
			otp, err := client.SendCode(ctx, email)
			if err != nil {
				return fmt.Errorf("sending code: %w", err)
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]string{
					"email": email, "otp_token": otp,
				})
			}
			fmt.Printf("Code sent to %s.\n", email)
			fmt.Printf("Finish with:\n  plaud login --email %s --otp-token %s --code <code>\n", email, otp)
			return nil
		}

		// If the handle and the code are both known, there is nothing to prompt for.
		if otpToken != "" && code != "" {
			fmt.Print("Authenticating... ")
			session, err := client.VerifyCode(ctx, otpToken, code)
			if err != nil {
				fmt.Println("failed.")
				return fmt.Errorf("login failed: %w", err)
			}
			fmt.Println("ok.")
			return saveSession(session)
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
		session, err := client.VerifyCode(ctx, otp, code)
		if err != nil {
			fmt.Println("failed.")
			return fmt.Errorf("login failed: %w", err)
		}
		fmt.Println("ok.")

		return saveSession(session)
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
	session, err := client.PasswordLogin(cmd.Context(), email, password)
	if err != nil {
		fmt.Println("failed.")
		return err
	}
	fmt.Println("ok.")
	return saveSession(session)
}

// readPassword takes the password from the environment or from stdin.
//
// It is never prompted for. This command runs where nobody is watching far
// more often than not, and a prompt there is a process that hangs rather than
// one that says what is missing.
func readPassword() (string, error) {
	if env := os.Getenv("PLAUD_PASSWORD"); env != "" {
		return env, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading the password from stdin: %w", err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", fmt.Errorf("--password reads the password from stdin or from PLAUD_PASSWORD, and got neither")
	}
	return password, nil
}

// saveToken records a bearer token handed over from somewhere else, which is
// what --token and PLAUD_TOKEN carry.
func saveToken(token string) error {
	cfg.AccessToken = token
	return persist("Token saved. You're logged in.")
}

// saveSession records what a login produced. From v3 that is a pair of
// cookies rather than a bearer token, so there is nothing to put in
// AccessToken and the old one is cleared rather than left to be sent
// alongside a session it has nothing to do with.
func saveSession(session *api.Session) error {
	cfg.Session = session
	if session.Valid() {
		cfg.AccessToken = ""
	}

	line := "Signed in."
	if at := session.Expiry(); at != nil {
		line = fmt.Sprintf("Signed in. The session lasts until %s, and is renewed as it is used.",
			at.Format("2006-01-02 15:04"))
	}
	return persist(line)
}

func persist(said string) error {
	cfg.BaseURL = client.BaseURL
	cfg.EnsureDeviceID()

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println(said)
	return nil
}

func init() {
	loginCmd.Flags().StringVar(&tokenFlag, "token", "", "use an existing access token (e.g. from browser DevTools)")
	loginCmd.Flags().StringVar(&emailFlag, "email", "", "account email (also read from PLAUD_EMAIL)")
	loginCmd.Flags().BoolVar(&passwordFlag, "password", false, "authenticate with a password, read from stdin or PLAUD_PASSWORD")
	loginCmd.Flags().BoolVar(&sendCodeFlag, "send-code", false, "send the login code and print the handle to finish with")
	loginCmd.Flags().StringVar(&otpTokenFlag, "otp-token", "", "handle returned by --send-code (also PLAUD_OTP_TOKEN)")
	loginCmd.Flags().StringVar(&codeFlag, "code", "", "the code that arrived by email (also PLAUD_CODE)")
	loginCmd.Flags().BoolVar(&migrateFlag, "migrate", false, "replace a pre-v3 bearer token with a session, refusing when there is nothing to replace")
	rootCmd.AddCommand(loginCmd)
}
