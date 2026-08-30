package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/auth"
	"github.com/jaisonerick/plaud-cli/internal/config"
	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check both sign-ins and this repository's setup",
	Long: `Everything that has to be true for a transcript to reach this repository.

There are two accounts and they answer different questions: the Plaud one says
which recordings can be read, and the Google one says whether anything can be
transcribed. Being signed in to one and not the other fails halfway through a
task, so they are reported on separate lines.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		binary, _ := os.Executable()

		plaud := plaudAccount(ctx)
		service := serviceAccount(ctx)
		r, err := repository()
		if err != nil {
			return err
		}

		lines := []string{
			fmt.Sprintf("plaud CLI     %s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH),
			fmt.Sprintf("  at          %s", binary),
			fmt.Sprintf("  plaud       %s", plaud),
		}
		// The scheme and the expiry are the Plaud sign-in's, so they sit under
		// the Plaud account rather than after the service, where they read as
		// facts about the wrong one of the two.
		lines = append(lines, signInLines()...)
		lines = append(lines, fmt.Sprintf("  service     %s", service))

		declared := "none — nothing declares where a transcript belongs here"
		if r.Declares() {
			declared = r.File
		}
		mode := "ad-hoc"
		if r.KeepsCatalog() {
			mode = "catalog"
		}
		lines = append(lines,
			fmt.Sprintf("repository    %s", r.Root),
			fmt.Sprintf("  declared    %s", declared),
			fmt.Sprintf("  mode        %s", mode),
			fmt.Sprintf("  context     %s", contextLine(r)),
		)
		if r.Filing != "" {
			lines = append(lines, fmt.Sprintf("  filing      %s", r.Filing))
		}
		fmt.Println(strings.Join(lines, "\n"))

		switch {
		case strings.HasPrefix(plaud, "NOT"):
			fmt.Fprint(os.Stderr, "\n"+loginGuidance)
		case cfg.Superseded():
			fmt.Fprint(os.Stderr, "\n"+migrationGuidance)
		}
		return nil
	},
}

// contextLine says whether the document that settles how names are spelt is
// there. A declaration pointing at a file nobody wrote is worth catching here
// rather than on the first fetch.
func contextLine(r *repo.Config) string {
	if r.Context == "" {
		return "none — pass --context when fetching"
	}
	if _, err := os.Stat(r.Context); err != nil {
		return r.Rel(r.Context) + "  <-- declared, and not there"
	}
	return r.Rel(r.Context)
}

func plaudAccount(ctx context.Context) string {
	user, err := client.GetMe(ctx)
	if err != nil {
		return "NOT AUTHENTICATED: " + err.Error()
	}
	source := "~/.config/plaud/token.json"
	if os.Getenv("PLAUD_TOKEN") != "" {
		source = "PLAUD_TOKEN"
	}
	return fmt.Sprintf("%s, from %s", user.Email, source)
}

func serviceAccount(ctx context.Context) string {
	session, err := auth.LoadSession()
	if err != nil {
		return "NOT SIGNED IN: " + err.Error()
	}
	if session == nil || session.RefreshToken == "" {
		return "NOT SIGNED IN: run 'plaud auth login'"
	}
	config, err := auth.FetchConfig(ctx, whisperEndpoint())
	if err != nil {
		return fmt.Sprintf("%s, and the service did not answer: %v", session.Email, err)
	}
	if _, err := auth.IDToken(ctx, config, session); err != nil {
		return fmt.Sprintf("%s, and the sign-in no longer works: %v", session.Email, err)
	}
	return session.Email
}

// signInLines report which scheme the Plaud credential uses and when it lapses.
//
// Which scheme it is has to be said out loud. The same date means opposite
// things either side of v3 — a session lasts a day and buys itself another on
// every call, a bearer token runs down for months and nothing here refreshes
// it — so a date alone leaves a reader unable to tell a fact from a deadline.
func signInLines() []string {
	scheme := cfg.Scheme()

	// The pointer carries the command rather than referring to what is
	// printed underneath: a token that has already lapsed is answered with the
	// sign-in guidance instead, and a marker pointing at a block that is not
	// there is worse than no marker.
	note := ""
	if cfg.Superseded() {
		note = "  <-- superseded: plaud login --migrate"
	}
	lines := []string{fmt.Sprintf("  scheme      %s%s", scheme, note)}

	expiry := tokenExpiry()
	if expiry == nil {
		return lines
	}
	if scheme == config.SessionScheme {
		// A session renews itself as it is used, so its expiry is a fact
		// rather than a deadline and needs no warning.
		return append(lines, fmt.Sprintf("  session     until %s, renewed as it is used",
			expiry.Format("2006-01-02 15:04")))
	}

	left := int(time.Until(*expiry).Hours() / 24)
	warn := ""
	if left < 21 {
		warn = "  <-- renew it, nothing here refreshes it"
	}
	return append(lines, fmt.Sprintf("  expires     %s (%d days)%s", expiry.Format("2006-01-02"), left, warn))
}

// tokenExpiry reads when the Plaud credential stops working. Which of the two
// schemes produced that date is Scheme's to say, not this function's.
func tokenExpiry() *time.Time {
	if at := cfg.Session.Expiry(); at != nil {
		return at
	}

	parts := strings.Split(cfg.AccessToken, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return nil
	}
	at := time.Unix(claims.Exp, 0)
	return &at
}

const loginGuidance = `Nobody is signed in to Plaud. Do it for the user, in the conversation, and ask
for the emailed code rather than their password:

  1. plaud login --send-code --email <their email> --json   # prints otp_token
  2. ask for the six digits that just arrived in their inbox
  3. plaud login --email <email> --otp-token <otp_token> --code <code>

An existing token can also arrive in PLAUD_TOKEN, which needs no file on disk.
`

const migrationGuidance = `This sign-in is the bearer token from before v3, and nothing renews it: when it
lapses every call is refused until somebody signs in again. What a login hands
over now is a session, which buys itself another on every call.

Replace it, asking for the emailed code rather than the password:

  1. plaud login --migrate --send-code --email <their email> --json   # prints otp_token
  2. ask for the six digits that just arrived in their inbox
  3. plaud login --migrate --email <email> --otp-token <otp_token> --code <code>
`

func init() {
	rootCmd.AddCommand(doctorCmd)
}
