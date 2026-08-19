package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/spf13/cobra"
)

var (
	speakerCompany  string
	renameCompany   string
	speakerListLong bool
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage who the transcription service recognises",
	Long: `People live on the service, not on this machine, and everyone signed in
shares them. A person is a first name, a last name and a company; transcripts
name them as "First Last (Company)".`,
}

var speakerNameCmd = &cobra.Command{
	Use:   "name <recording-id> <speaker-label> <first-last>",
	Short: "Say who one of the voices in a recording is",
	Long: `Give a name to one of the voices the transcription separated.

The voices stay on the service, which knows them by the recording they came
from — the same id 'plaud list' shows.

A first and last name are required, and so is --company: a lone first name
names whichever Amanda the person typing had in mind, and the store is shared.
Anything past the second word is dropped, the company having a field now.

Example:
  plaud speaker name e348561a6b26d65c9 SPEAKER_01 "Jaison Erick" --company NexaEdge`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		recordingID, label, name := args[0], args[1], strings.TrimSpace(args[2])

		if err := requireFullName(name); err != nil {
			return err
		}
		if strings.TrimSpace(speakerCompany) == "" {
			return fmt.Errorf("--company is required, so a transcript can always name one")
		}
		warnIfTrimmed(name)

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		people, err := whisper.ListPeople(ctx)
		if err != nil {
			return fmt.Errorf("listing people: %w", err)
		}
		existing := make([]string, len(people))
		for i, p := range people {
			existing[i] = p.Name
		}

		name, err = confirmName(name, existing)
		if err != nil || name == "" {
			return err
		}

		person, err := whisper.NameSpeaker(ctx, recordingID, label, name, speakerCompany)
		if err != nil {
			return err
		}

		fmt.Printf("%s is %s — %d voice(s) on file\n", label, person.Display(), person.Voices)
		return nil
	},
}

var speakerAliasCmd = &cobra.Command{
	Use:   "alias <spelling> <first-last>",
	Short: "Record how transcripts spell somebody",
	Long: `Transcripts call people whatever the person typing felt like — "luca",
"Vic", "Amanda" — and only somebody who knows them can say who that is.

The answer is kept on the service, so it is given once and everybody enrolling
afterwards benefits. The person has to exist already: name a voice of theirs
first.

Example:
  plaud speaker alias "Vic" "Victoria Dinie"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		spelling, name := args[0], strings.TrimSpace(args[1])
		if err := requireFullName(name); err != nil {
			return err
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		if err := whisper.SetAlias(cmd.Context(), spelling, name); err != nil {
			return err
		}
		fmt.Printf("Transcripts saying %q mean %q\n", spelling, name)
		return nil
	},
}

var speakerRenameCmd = &cobra.Command{
	Use:   "rename <current-name> <first-last>",
	Short: "Correct who somebody is",
	Long: `Move a person to a different name, carrying their voices with them.

Use it to give a first name its surname, to pull a company out of the name it
was glued to, or to join two spellings of one person.

Example:
  plaud speaker rename "Victoria Dinie" "Victoria Sobrenome" --company Dinie`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		old, name := args[0], strings.TrimSpace(args[1])
		if err := requireFullName(name); err != nil {
			return err
		}
		if strings.TrimSpace(renameCompany) == "" {
			return fmt.Errorf("--company is required")
		}
		warnIfTrimmed(name)

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		person, err := whisper.RenamePerson(cmd.Context(), old, name, renameCompany)
		if err != nil {
			return err
		}
		fmt.Printf("%q is now %s\n", old, person.Display())
		return nil
	},
}

var speakerForgetCmd = &cobra.Command{
	Use:   "forget <first-last>",
	Short: "Drop somebody the recogniser learned wrongly",
	Long: `Remove a person and every voice stored for them.

A voice learned from the wrong person is not inert: it is compared against
every transcription from then on, and will keep claiming somebody else's until
it is dropped.

Example:
  plaud speaker forget "Amanda Destro"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		if err := whisper.ForgetPerson(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Dropped %q and every voice of theirs\n", args[0])
		return nil
	},
}

var speakerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List everybody the service recognises",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		people, err := whisper.ListPeople(cmd.Context())
		if err != nil {
			return fmt.Errorf("listing people: %w", err)
		}
		if len(people) == 0 {
			fmt.Println("Nobody is known yet.")
			return nil
		}

		if speakerListLong {
			fmt.Printf("%-24s %-16s %6s  %s\n", "NAME", "COMPANY", "VOICES", "ADDED BY")
			for _, p := range people {
				fmt.Printf("%-24s %-16s %6d  %s\n", truncate(p.Name, 24), truncate(p.Company, 16), p.Voices, p.CreatedBy)
			}
			return nil
		}
		for _, p := range people {
			fmt.Printf("  %-40s %d voice(s)\n", p.Display(), p.Voices)
		}
		return nil
	},
}

// requireFullName keeps a lone first name out of a store shared with everyone
// else using the service, where "Amanda" identifies nobody in particular.
func requireFullName(name string) error {
	if name == "" {
		return fmt.Errorf("the name cannot be empty")
	}
	if !speaker.IsFull(name) {
		return fmt.Errorf("%q is a first name — give a first and last name, so the person means the same to everyone using the service", name)
	}
	return nil
}

// warnIfTrimmed says so when a name carries more than the two words kept.
func warnIfTrimmed(name string) {
	if parts := strings.Fields(name); len(parts) > 2 {
		fmt.Fprintf(os.Stderr, "Keeping %q as %q; the rest belongs in --company.\n",
			name, strings.Join(parts[:2], " "))
	}
}

// confirmName asks before a new spelling of an existing person is created,
// and returns the name to register, or "" when the operator backs out.
func confirmName(name string, existing []string) (string, error) {
	matches := speaker.Similar(name, existing)
	if len(matches) == 0 {
		return name, nil
	}
	if matches[0].Same {
		return matches[0].Name, nil
	}

	fmt.Fprintf(os.Stderr, "%q resembles a name already known:\n", name)
	for i, m := range matches {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, m.Name)
	}
	fmt.Fprintf(os.Stderr, "  n) keep %q as a new, separate person\n", name)
	fmt.Fprintf(os.Stderr, "Which is it? [1] ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		// Nobody is at the keyboard, and merging two people on a default is
		// exactly the mistake this prompt exists to prevent.
		return "", fmt.Errorf("cannot ask which name is meant with no terminal attached — name it exactly as it is already known")
	}
	switch answer := strings.TrimSpace(strings.ToLower(line)); answer {
	case "":
		return matches[0].Name, nil
	case "n":
		return name, nil
	default:
		var choice int
		if _, err := fmt.Sscanf(answer, "%d", &choice); err != nil || choice < 1 || choice > len(matches) {
			return "", fmt.Errorf("%q is not one of the options", answer)
		}
		return matches[choice-1].Name, nil
	}
}

func init() {
	speakerNameCmd.Flags().StringVar(&speakerCompany, "company", "", "the company this person is from (required)")
	speakerRenameCmd.Flags().StringVar(&renameCompany, "company", "", "the company this person is from (required)")
	speakerListCmd.Flags().BoolVar(&speakerListLong, "long", false, "show the company and who added each person")

	speakerCmd.AddCommand(speakerNameCmd)
	speakerCmd.AddCommand(speakerAliasCmd)
	speakerCmd.AddCommand(speakerRenameCmd)
	speakerCmd.AddCommand(speakerForgetCmd)
	speakerCmd.AddCommand(speakerListCmd)
	speakerCmd.AddCommand(speakerEnrollCmd)
	rootCmd.AddCommand(speakerCmd)
}
