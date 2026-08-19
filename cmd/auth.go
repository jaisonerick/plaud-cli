package cmd

import (
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
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := auth.FetchConfig(ctx, whisperEndpoint())
		if err != nil {
			return err
		}

		session, err := auth.SignIn(ctx, cfg, os.Stderr)
		if err != nil {
			return err
		}
		if err := auth.SaveSession(session); err != nil {
			return fmt.Errorf("saving the sign-in: %w", err)
		}

		fmt.Printf("Signed in as %s\n", session.Email)
		if domain := domainOf(session.Email); len(cfg.Domains) > 0 && !contains(cfg.Domains, domain) {
			fmt.Fprintf(os.Stderr, "Warning: the service serves %s, so this account will be refused.\n",
				strings.Join(cfg.Domains, ", "))
		}
		return nil
	},
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

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}
