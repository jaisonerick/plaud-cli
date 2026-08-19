package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage known speaker identities",
}

var speakerNameCmd = &cobra.Command{
	Use:   "name <transcript> <speaker-label> <name>",
	Short: "Name a speaker from a saved transcript",
	Long: `Give a name to one of the speakers in a transcript this CLI produced.

The voice is read from the speaker file saved beside the transcript, so this
works on anything already on disk — no audio is uploaded and the recording
need not still be known to the server. From then on, that voice is recognised
in new transcriptions.

If the name resembles one already known, you are asked before a second spelling
of the same person is created.

Example:
  plaud speaker name ./transcript.md SPEAKER_01 "Jaison Erick"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		path, label, name := args[0], args[1], strings.TrimSpace(args[2])

		if name == "" {
			return fmt.Errorf("the name cannot be empty")
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		sidecar := transcript.SpeakerSidecarPath(path)
		file, err := transcript.ReadSpeakerFile(sidecar)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no speaker file beside %s — expected %s, which transcriptions write from this version on", path, sidecar)
			}
			return err
		}

		entry, ok := file.Speakers[label]
		if !ok {
			return fmt.Errorf("%s holds no speaker %q (it has: %s)", sidecar, label, strings.Join(file.Labels(), ", "))
		}
		if len(entry.Embedding) == 0 {
			return fmt.Errorf("%s has no voice recorded for %q, so there is nothing to recognise it by", sidecar, label)
		}

		known, err := whisper.ListKnownSpeakers(ctx)
		if err != nil {
			return fmt.Errorf("listing known speakers: %w", err)
		}
		existing := make([]string, len(known))
		for i, k := range known {
			existing[i] = k.Name
		}

		name, err = confirmName(name, existing)
		if err != nil || name == "" {
			return err
		}

		samples, err := whisper.AddKnownSpeaker(ctx, name, entry.Embedding)
		if err != nil {
			return fmt.Errorf("registering speaker: %w", err)
		}

		entry.Name = name
		file.Speakers[label] = entry
		if err := transcript.WriteSpeakerFile(sidecar, *file); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update %s: %v\n", sidecar, err)
		}

		fmt.Printf("%s is now %q (%d voice sample(s) on file)\n", label, name, samples)
		return nil
	},
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

var speakerSetCmd = &cobra.Command{
	Use:   "set <audio-id> <speaker-id> <name>",
	Short: "Name a speaker the server still remembers",
	Long: `Name a speaker from a transcription the server has not yet forgotten.

Prefer 'plaud speaker name', which reads the voice from the transcript on disk
and therefore keeps working once the server has moved on.

Example:
  plaud speaker set abc-123-def SPEAKER_00 "Alice"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		audioID, speakerID, name := args[0], args[1], args[2]
		if err := whisper.SetSpeakerName(cmd.Context(), audioID, speakerID, name); err != nil {
			return fmt.Errorf("setting speaker name: %w", err)
		}
		fmt.Printf("Speaker %q registered from audio %s/%s\n", name, audioID, speakerID)
		return nil
	},
}

var speakerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all known speakers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		speakers, err := whisper.ListKnownSpeakers(cmd.Context())
		if err != nil {
			return fmt.Errorf("listing speakers: %w", err)
		}
		if len(speakers) == 0 {
			fmt.Println("No known speakers.")
			return nil
		}

		fmt.Println("Known speakers:")
		for _, s := range speakers {
			fmt.Printf("  %-30s %d sample(s)\n", s.Name, s.Samples)
		}
		return nil
	},
}

func init() {
	speakerCmd.AddCommand(speakerNameCmd)
	speakerCmd.AddCommand(speakerSetCmd)
	speakerCmd.AddCommand(speakerListCmd)
	speakerCmd.AddCommand(speakerEnrollCmd)
	rootCmd.AddCommand(speakerCmd)
}
