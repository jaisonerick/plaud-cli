package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/identify"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var speakerPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "The voices in this repository's transcripts that nobody has named",
	Long: `Count the voices still called SPEAKER_nn in the transcripts here.

It reads the files and nothing else: a transcript says which recording it came
from and which voice each name in it stands for, so this costs no request and
no account.

Naming them is 'plaud speaker identify', which opens a page that plays each
voice and takes the name.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}
		voices, unreachable, err := identify.Pending(r.Root)
		if err != nil {
			return err
		}

		if jsonOut {
			out, err := json.MarshalIndent(summarise(voices, unreachable), "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}
		if len(voices) == 0 && len(unreachable) == 0 {
			fmt.Fprintln(os.Stderr, "Every voice in the transcripts here has a name.")
			return nil
		}

		byFile := map[string][]string{}
		for _, v := range voices {
			byFile[v.File] = append(byFile[v.File], v.Label)
		}
		files := make([]string, 0, len(byFile))
		for file := range byFile {
			files = append(files, file)
		}
		sort.Strings(files)
		for _, file := range files {
			fmt.Printf("%-4d %s  (%s)\n", len(byFile[file]), r.Rel(file), strings.Join(byFile[file], ", "))
		}
		if len(voices) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d voice(s) across %d transcript(s) have no name. Run 'plaud speaker identify'.\n",
				len(voices), len(files))
		}
		if len(unreachable) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d transcript(s) hold unnamed voices and do not say which recording they came from,\n"+
				"so there is nothing to ask about. 'plaud sync' stamps that in and decodes nothing:\n", len(unreachable))
			for _, file := range unreachable {
				fmt.Fprintf(os.Stderr, "  %s\n", r.Rel(file))
			}
		}
		return nil
	},
}

// summarise is what a routine reads: the counts it would act on, and the
// voices themselves for anything that wants to show them.
func summarise(voices []identify.Voice, unreachable []string) map[string]any {
	files := map[string]bool{}
	recordings := map[string]bool{}
	for _, v := range voices {
		files[v.File] = true
		recordings[v.Recording] = true
	}
	return map[string]any{
		"pending":     len(voices),
		"transcripts": len(files),
		"recordings":  len(recordings),
		"voices":      voices,
		"unreachable": unreachable,
	}
}

var speakerIdentifyCmd = &cobra.Command{
	Use:   "identify",
	Short: "Open a page that plays each unnamed voice and takes its name",
	Long: `Put people to the voices still called SPEAKER_nn in the transcripts here.

A page opens in the browser with, for each voice, the stretches of the
recording where it is the one speaking, and a field for the name. Names already
known are offered as you type, because two spellings of one person are two
people.

Each name is registered the moment it is typed, so a tab closed halfway leaves
the voices already named named. When the page is finished, the transcripts here
are rewritten with the names.

Nothing is inferred. A voice nobody in the room can place is left alone, which
is what the blank field is for: a guess puts a wrong name on a voice for
everyone else using the service too.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		r, err := repository()
		if err != nil {
			return err
		}
		voices, _, err := identify.Pending(r.Root)
		if err != nil {
			return err
		}
		if len(voices) == 0 {
			fmt.Fprintln(os.Stderr, "Every voice in the transcripts here has a name.")
			return nil
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}
		known, err := whisper.ListPeople(ctx)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(known))
		for _, person := range known {
			names = append(names, person.Display())
		}
		sort.Strings(names)

		named, err := identify.RunServer(ctx, identify.Config{
			Voices: voices,
			Known:  names,
			Audio: func(ctx context.Context, recording string) ([]byte, error) {
				url, err := client.GetTempURL(ctx, recording)
				if err != nil {
					return nil, fmt.Errorf("getting the audio: %w", err)
				}
				return client.FetchFile(ctx, url, nil)
			},
			Name: func(ctx context.Context, v identify.Voice, name, company string, surnameUnknown bool) (string, error) {
				person, err := whisper.NameSpeaker(ctx, v.Recording, v.ID, name, company, surnameUnknown)
				if err != nil {
					return "", err
				}
				return person.Display(), nil
			},
		})
		if err != nil {
			return err
		}
		if len(named) == 0 {
			fmt.Fprintln(os.Stderr, "Nothing was named.")
			return nil
		}

		fmt.Fprintf(os.Stderr, "\n%d voice(s) named:\n", len(named))
		for _, settled := range named {
			fmt.Fprintf(os.Stderr, "  %s is %s\n", settled.Voice.Label, settled.Person)
		}
		return settleFiles(ctx, whisper, r.Root, named)
	},
}

// settleFiles rewrites the transcripts a naming touched. The service is what
// holds who a voice is, so this asks it rather than writing down what was just
// typed: a voice named here may be the same person as one named an hour ago,
// and what the file should say is whoever the service settles on now.
func settleFiles(ctx context.Context, whisper *modal.HTTPClient, root string, named identify.Named) error {
	for _, file := range named.Files() {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		recording := transcript.ReadField(string(content), transcript.RecordingKey)
		if recording == "" {
			continue
		}
		job := job{recording: api.RecordingSimple{ID: recording, Name: file}, dest: file, how: filing{format: "md"}, written: true}
		if err := job.refreshNames(ctx, whisper); err != nil {
			fmt.Fprintf(os.Stderr, "  could not settle %s: %v\n", file, err)
		}
	}
	return nil
}

func init() {
	speakerCmd.AddCommand(speakerPendingCmd, speakerIdentifyCmd)
}
