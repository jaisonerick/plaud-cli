package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/auth"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Sign in to the transcription service",
	Long: `The transcription service is shared, and a Google account is what gets you in.

Nothing from the cloud it runs on is needed: no account there, no keys. Sign in
once and the CLI keeps the session, refreshing it by itself.

Only accounts on the domains the service serves are let in.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in with Google",
	Long: `Sign in to the transcription service.

Prints a short code and a URL, then waits. Whoever is signing in opens that URL
on any device — a phone will do — types the code, and this returns. Nothing
here opens a browser or listens on a port, so it behaves the same in a
terminal, over ssh, in a container, or driven by an assistant on somebody's
behalf.

With --json it prints one JSON object per line: the code first, so it can be
shown immediately, and the outcome when the sign-in finishes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := auth.FetchConfig(ctx, whisperEndpoint())
		if err != nil {
			return err
		}

		if authBrowser {
			session, err := auth.SignIn(ctx, cfg, os.Stderr)
			if err != nil {
				return err
			}
			return finishLogin(session, cfg)
		}

		pending, err := auth.StartDevice(ctx, cfg)
		if err != nil {
			return err
		}

		// Printed and flushed before the wait, because whoever is watching this
		// output has to act on it for the wait to ever end.
		if authJSON {
			emit(map[string]any{
				"status":           "pending",
				"user_code":        pending.UserCode,
				"verification_url": pending.VerificationURL,
				"expires_in":       pending.ExpiresIn,
			})
		} else {
			fmt.Printf("Open %s and enter this code:\n\n    %s\n\nWaiting...\n",
				pending.VerificationURL, pending.UserCode)
		}

		session, err := auth.AwaitDevice(ctx, cfg, pending)
		if err != nil {
			if authJSON {
				emit(map[string]any{"status": "failed", "error": err.Error()})
			}
			return err
		}
		return finishLogin(session, cfg)
	},
}

// finishLogin stores the session and says who it belongs to.
func finishLogin(session *auth.Session, cfg *auth.Config) error {
	if err := auth.SaveSession(session); err != nil {
		return fmt.Errorf("saving the sign-in: %w", err)
	}

	domain := domainOf(session.Email)
	served := len(cfg.Domains) == 0 || contains(cfg.Domains, domain)

	if authJSON {
		emit(map[string]any{"status": "signed-in", "email": session.Email, "served": served})
	} else {
		fmt.Printf("Signed in as %s\n", session.Email)
	}
	if !served {
		fmt.Fprintf(os.Stderr, "Warning: the service serves %s, so this account will be refused.\n",
			strings.Join(cfg.Domains, ", "))
	}
	return nil
}

// emit writes one JSON object per line, so a caller can read the code without
// waiting for the sign-in it is about to describe.
func emit(event map[string]any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Println(string(data))
	// Flushed because the reader has to act on this line for the wait that
	// follows it to ever end.
	_ = os.Stdout.Sync()
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show who is signed in",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := auth.LoadSession()
		if err != nil {
			return err
		}
		if session == nil || session.RefreshToken == "" {
			fmt.Println("Not signed in. Run 'plaud auth login'.")
			return nil
		}

		fmt.Printf("Signed in as %s\n", session.Email)

		// A stored session says nothing about whether it still works, and the
		// difference only shows up mid-transcription otherwise.
		cfg, err := auth.FetchConfig(cmd.Context(), whisperEndpoint())
		if err != nil {
			return err
		}
		if _, err := auth.IDToken(cmd.Context(), cfg, session); err != nil {
			return err
		}
		fmt.Println("The sign-in still works.")
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Forget the stored sign-in",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.ForgetSession(); err != nil {
			return err
		}
		fmt.Println("Signed out.")
		return nil
	},
}

// whisperEndpoint is where the service lives for this run.
func whisperEndpoint() string {
	return modal.NewHTTPClient(cfg.WhisperURL, nil).EndpointURL
}

func domainOf(email string) string {
	_, domain, _ := strings.Cut(email, "@")
	return domain
}

var (
	authJSON    bool
	authBrowser bool
)

func init() {
	authLoginCmd.Flags().BoolVar(&authJSON, "json", false, "print the code and the outcome as JSON, one object per line")
	authLoginCmd.Flags().BoolVar(&authBrowser, "browser", false, "sign in through a browser on this machine instead")

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}
