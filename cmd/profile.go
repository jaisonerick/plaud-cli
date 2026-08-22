package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/spf13/cobra"
)

var (
	profileTag      string
	profileDest     string
	profileName     string
	profileLanguage string
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Say which of your recordings feed a repository's profile",
	Long: `A profile has two halves, and they belong to different people.

Where transcripts of a kind go, what they are called and what their front
matter carries is the repository's, declared in ` + repo.FileName + ` and true
for everyone working in it.

Which recordings are of that kind is yours. A Plaud tag lives in one person's
account, so committing it hands everyone else a profile that selects nothing.
That half goes in your own settings, which are never committed, and this is
how it is written.`,
}

var profileSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Point one of this repository's profiles at your recordings",
	Long: `Record, for yourself alone, how a profile of this repository finds your
recordings.

The profile does not have to exist in ` + repo.FileName + `: a repository that
declares nothing still gets a profile made entirely of your own settings, which
is what to reach for when where the transcripts go is your business too.

Examples:
  plaud profile set cerc --tag "PPFX - Amanda"
  plaud profile set cerc --tag "CERC" --dest "reunioes/{year}"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if profileTag == "" && profileDest == "" && profileName == "" && profileLanguage == "" {
			return fmt.Errorf("nothing to set: pass at least one of --tag, --dest, --name, --language")
		}
		r, err := repository()
		if err != nil {
			return err
		}
		settings, err := repo.LoadSettings()
		if err != nil {
			return err
		}

		mine := settings.For(r.Identity, true)
		if mine.Profiles == nil {
			mine.Profiles = map[string]repo.Profile{}
		}
		profile := mine.Profiles[args[0]]
		if profileTag != "" {
			profile.Tag = profileTag
		}
		if profileDest != "" {
			profile.Dest = profileDest
		}
		if profileName != "" {
			profile.Name = profileName
		}
		if profileLanguage != "" {
			profile.Language = profileLanguage
		}
		mine.Profiles[args[0]] = profile

		if err := settings.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote %s for %s in %s\n", args[0], r.Identity, settings.Path())
		return nil
	},
}

var profileUnsetCmd = &cobra.Command{
	Use:   "unset <name>",
	Short: "Forget what you set about a profile here",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		settings, err := repo.LoadSettings()
		if err != nil {
			return err
		}

		mine := settings.For(r.Identity, false)
		if mine == nil || mine.Profiles == nil {
			return fmt.Errorf("you have set nothing about %s", r.Identity)
		}
		if _, ok := mine.Profiles[args[0]]; !ok {
			return fmt.Errorf("you have set nothing about the profile %q here", args[0])
		}
		delete(mine.Profiles, args[0])

		if err := settings.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Forgot %s for %s\n", args[0], r.Identity)
		return nil
	},
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "The profiles of this repository, and which of them can select anything",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		names := r.ProfileNames()
		if len(names) == 0 {
			fmt.Fprintf(os.Stderr, "No profiles here. %s declares them, and 'plaud profile set' says\n"+
				"which of your recordings feed each one.\n", repo.FileName)
			return nil
		}

		sort.Strings(names)
		for _, name := range names {
			profile, _ := r.Profile(name)
			if profile.Tag == "" {
				fmt.Printf("%-16s no tag — 'plaud profile set %s --tag \"<your tag>\"'\n", name, name)
				continue
			}
			fmt.Printf("%-16s tag %q (%s)\n", name, profile.Tag, r.Where("profiles."+name+".tag"))
		}
		return nil
	},
}

// missingTag says what to do about a profile nothing selects for. Reaching the
// account with no tag is the one failure this split introduces, so it is worth
// naming rather than answering with an empty list.
func missingTag(r *repo.Config, name string) error {
	return fmt.Errorf("the profile %q does not say which of your recordings it selects.\n"+
		"  Set it:  plaud profile set %s --tag \"<the tag in your Plaud>\"\n"+
		"  Tags:    plaud tag list\n"+
		"A tag lives in one person's account, so %s cannot carry it for everyone.",
		name, name, strings.TrimPrefix(r.Rel(r.File), "./"))
}

func init() {
	f := profileSetCmd.Flags()
	f.StringVar(&profileTag, "tag", "", "the tag in your Plaud account that selects these recordings")
	f.StringVar(&profileDest, "dest", "", "where these transcripts go, overriding the repository")
	f.StringVar(&profileName, "name", "", "what these transcripts are called, overriding the repository")
	f.StringVar(&profileLanguage, "language", "", "the language these recordings are in")

	profileCmd.AddCommand(profileSetCmd, profileUnsetCmd, profileListCmd)
	rootCmd.AddCommand(profileCmd)
}
