package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/spf13/cobra"
)

var (
	speakerCompany   string
	renameCompany    string
	speakerNoSurname bool
	speakerNewPerson bool
	renameNoSurname  bool
	speakerListLong  bool
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
  plaud speaker name e348561a6b26d65c9 SPEAKER_01 "Jaison Erick" --company NexaEdge

The label is one run's numbering; the id in a transcript's front matter is the
voice itself, and either works here. A name that resembles somebody already
known is refused rather than registered, because two spellings of one person
are two people and only a person can put them back together; --new-person says
it really is somebody else.`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		recordingID, label, name := args[0], args[1], strings.TrimSpace(args[2])

		if err := requireFullName(name, speakerNoSurname); err != nil {
			return err
		}
		if strings.TrimSpace(speakerCompany) == "" {
			return fmt.Errorf("--company is required, so a transcript can always name one")
		}

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

		if !speakerNewPerson {
			if name, err = settleName(name, existing); err != nil {
				return err
			}
		}

		person, err := whisper.NameSpeaker(ctx, recordingID, label, name, speakerCompany, speakerNoSurname)
		if err != nil {
			return err
		}

		fmt.Printf("%s is %s — %d voice(s) on file\n", label, person.Display(), person.Voices)
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
		if err := requireFullName(name, renameNoSurname); err != nil {
			return err
		}
		if strings.TrimSpace(renameCompany) == "" {
			return fmt.Errorf("--company is required")
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		person, err := whisper.RenamePerson(cmd.Context(), old, name, renameCompany, renameNoSurname)
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
func requireFullName(name string, surnameUnknown bool) error {
	if name == "" {
		return fmt.Errorf("the name cannot be empty")
	}
	// Demanding a surname exists to catch the one typed without thinking. A
	// surname nobody knows is a different thing, and saying so is the point.
	if !speaker.IsFull(name) && !surnameUnknown {
		return fmt.Errorf("%q is a first name — give a surname so the person means the same to everyone, or pass --surname-unknown", name)
	}
	return nil
}

// settleName refuses a new spelling of somebody already known.
//
// It used to ask, which needed a terminal, and this command runs where nobody
// is watching far more often than not: a question there is a process that
// hangs. Refusing costs one retry with the exact name, where the alternative
// is a second person nobody notices until two halves of the same voice are
// under two spellings and only a person can put them back.
func settleName(name string, existing []string) (string, error) {
	matches := speaker.Similar(name, existing)
	if len(matches) == 0 {
		return name, nil
	}
	if matches[0].Same {
		return matches[0].Name, nil
	}

	var known []string
	for _, m := range matches {
		known = append(known, strconv.Quote(m.Name))
	}
	return "", fmt.Errorf("%q resembles somebody already known: %s.\n"+
		"  If it is them, write the name exactly as it is above.\n"+
		"  If it is somebody else, pass --new-person.",
		name, strings.Join(known, ", "))
}

func init() {
	speakerNameCmd.Flags().StringVar(&speakerCompany, "company", "", "the company this person is from (required)")
	speakerNameCmd.Flags().BoolVar(&speakerNoSurname, "surname-unknown", false, "record somebody whose surname nobody knows; their company tells them apart")
	speakerNameCmd.Flags().BoolVar(&speakerNewPerson, "new-person", false, "register the name even though it resembles somebody known")
	speakerRenameCmd.Flags().StringVar(&renameCompany, "company", "", "the company this person is from (required)")
	speakerRenameCmd.Flags().BoolVar(&renameNoSurname, "surname-unknown", false, "record somebody whose surname nobody knows")
	speakerListCmd.Flags().BoolVar(&speakerListLong, "long", false, "show the company and who added each person")

	speakerCmd.AddCommand(speakerNameCmd)
	speakerCmd.AddCommand(speakerRenameCmd)
	speakerCmd.AddCommand(speakerForgetCmd)
	speakerCmd.AddCommand(speakerListCmd)
	speakerCmd.AddCommand(speakerTeachCmd)
	rootCmd.AddCommand(speakerCmd)
}
