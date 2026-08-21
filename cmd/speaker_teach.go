package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/spf13/cobra"
)

var (
	teachRangesFile string
	teachDryRun     bool
)

var speakerTeachCmd = &cobra.Command{
	Use:   "teach <recording-id>",
	Short: "Learn voices from the stretches of a recording you chose",
	Long: `Teach the recogniser from the exact stretches where each person speaks.

'speaker name' takes a voice the diarization separated, whole. Where it put two
people in one voice, that voice is the average of the two and naming it teaches
neither, so the speech is lost to the recogniser. This takes the stretches
instead, and the same recording still yields one clean sample per person.

The file lists each person and the stretches they speak, in milliseconds from
the start of the recording:

  [
    {"name": "Jaison Erick", "company": "NexaEdge", "ranges": [[262000, 271000]]},
    {"name": "Éricles Bento", "company": "CERC", "ranges": [[279000, 304000]]}
  ]

Somebody whose surname nobody knows carries "surname_unknown": true, as
'speaker name' does with its flag.

Examples:
  plaud speaker teach 8c8ede72f191ed6044d98c68a9e9df67 --ranges divisao.json --dry-run
  plaud speaker teach 8c8ede72f191ed6044d98c68a9e9df67 --ranges divisao.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		recordingID := args[0]

		specs, err := readSpeakerRanges(teachRangesFile)
		if err != nil {
			return err
		}

		for _, s := range specs {
			fmt.Printf("%-28s %d stretch(es), %s\n", personLine(s), len(s.Ranges), duration(speechIn(s)))
		}
		if teachDryRun {
			fmt.Println("\nDry run: no audio was fetched and nobody was taught.")
			return nil
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		audioData, err := fetchAudio(ctx, recordingID)
		if err != nil {
			return err
		}

		result, err := whisper.EnrollSpeakers(ctx, audioData, specs)
		if err != nil {
			return fmt.Errorf("teaching: %w", err)
		}

		fmt.Println()
		for name := range result.Enrolled {
			fmt.Printf("learned %s\n", name)
		}
		for name, why := range result.Skipped {
			fmt.Printf("skipped %s: %s\n", name, why)
		}
		return nil
	},
}

// readSpeakerRanges reads the people to teach and the stretches each one
// speaks. The file is refused rather than trimmed: a name or a range typed
// wrongly teaches a real person a voice that is not theirs.
func readSpeakerRanges(path string) ([]modal.SpeakerRanges, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("--ranges is required: teaching takes the stretches each person speaks")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the ranges: %w", err)
	}

	var specs []modal.SpeakerRanges
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%s names nobody to teach", path)
	}

	for i, s := range specs {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return nil, fmt.Errorf("the person at position %d has no name", i+1)
		}
		if !speaker.IsFull(name) && !s.SurnameUnknown {
			return nil, fmt.Errorf("%q is a first name — give a surname so the person means the same to everyone, or set \"surname_unknown\": true", name)
		}
		if strings.TrimSpace(s.Company) == "" {
			return nil, fmt.Errorf("%q has no company, and a transcript has to be able to name one", name)
		}
		if len(s.Ranges) == 0 {
			return nil, fmt.Errorf("%q has no ranges, so there is nothing to listen to", name)
		}
		for _, r := range s.Ranges {
			if r[1] <= r[0] {
				return nil, fmt.Errorf("%q has a range that ends before it starts: %d to %d", name, r[0], r[1])
			}
		}
		specs[i].Name = name
	}
	return specs, nil
}

// fetchAudio brings down the audio of a recording, which teaching needs and
// nothing else here does.
func fetchAudio(ctx context.Context, recordingID string) ([]byte, error) {
	tempURL, err := client.GetTempURL(ctx, recordingID)
	if err != nil {
		return nil, fmt.Errorf("getting audio URL: %w", err)
	}
	audioData, err := client.FetchFile(ctx, tempURL, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading audio: %w", err)
	}
	return audioData, nil
}

func speechIn(s modal.SpeakerRanges) int {
	total := 0
	for _, r := range s.Ranges {
		total += r[1] - r[0]
	}
	return total
}

func personLine(s modal.SpeakerRanges) string {
	return fmt.Sprintf("%s (%s)", s.Name, s.Company)
}

func duration(ms int) string {
	seconds := ms / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}

func init() {
	speakerTeachCmd.Flags().StringVar(&teachRangesFile, "ranges", "", "file listing each person and the stretches they speak (required)")
	speakerTeachCmd.Flags().BoolVar(&teachDryRun, "dry-run", false, "show what would be taught without fetching audio")
}
