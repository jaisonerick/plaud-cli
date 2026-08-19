package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/speaker"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	enrollMaxPerSpeaker int
	enrollMinSpeech     int
	enrollDryRun        bool
	enrollYes           bool
	enrollWorkers       int
)

// genericLabel matches the placeholder Plaud uses for a voice nobody named.
var genericLabel = regexp.MustCompile(`(?i)^(speaker|falante)[\s_-]*\d+$`)

var speakerEnrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Teach the recogniser from transcripts that already name their speakers",
	Long: `Seed speaker recognition from the recordings Plaud transcribed and attributed.

Those transcripts say who spoke when. This walks them, picks the recordings
where each person speaks most, and has the Whisper service listen to just
those stretches to learn the voice. Afterwards, new transcriptions recognise
those people by themselves.

Only audio for the recordings actually chosen is downloaded, so raising
--max-per-speaker costs a download and a GPU pass per extra recording.

Examples:
  plaud speaker enroll --dry-run
  plaud speaker enroll
  plaud speaker enroll --max-per-speaker 3`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		samples, err := gatherVoiceSamples(ctx)
		if err != nil {
			return err
		}
		if len(samples) == 0 {
			fmt.Println("No transcript names a speaker, so there is nothing to learn from.")
			return nil
		}

		known, err := whisper.ListKnownSpeakers(ctx)
		if err != nil {
			return fmt.Errorf("listing known speakers: %w", err)
		}
		knownNames := make([]string, len(known))
		for i, k := range known {
			knownNames[i] = k.Name
		}

		samples, err = reconcileNames(samples, knownNames)
		if err != nil {
			return err
		}

		byRecording, chosen := selectSamples(samples)
		reportPlan(samples, chosen)

		if enrollDryRun {
			fmt.Printf("\nDry run: nothing was downloaded or enrolled.\n")
			return nil
		}
		if len(byRecording) == 0 {
			return nil
		}

		return enrollRecordings(ctx, whisper, byRecording)
	},
}

// voiceSample is one person speaking in one recording, and where.
type voiceSample struct {
	name          string
	recordingID   string
	recordingName string
	speechMS      int
	ranges        [][2]int
}

// gatherVoiceSamples reads every Plaud transcript and returns the stretches
// each named person speaks, keyed by name.
func gatherVoiceSamples(ctx context.Context) (map[string][]voiceSample, error) {
	recordings, err := client.ListRecordings(ctx)
	if err != nil {
		return nil, err
	}

	var withTranscript []api.RecordingSimple
	for _, r := range recordings {
		if r.HasTranscript {
			withTranscript = append(withTranscript, r)
		}
	}
	fmt.Printf("Reading %d transcript(s) for names...\n", len(withTranscript))

	var (
		mu      sync.Mutex
		samples = map[string][]voiceSample{}
		failed  int
		wg      sync.WaitGroup
	)
	queue := make(chan api.RecordingSimple)

	for i := 0; i < enrollWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range queue {
				found, err := voiceSamplesIn(ctx, r)
				mu.Lock()
				if err != nil {
					failed++
				}
				for _, s := range found {
					samples[s.name] = append(samples[s.name], s)
				}
				mu.Unlock()
			}
		}()
	}
	for _, r := range withTranscript {
		queue <- r
	}
	close(queue)
	wg.Wait()

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "  (%d transcript(s) could not be read and were left out)\n", failed)
	}
	return samples, nil
}

// voiceSamplesIn pulls the named speakers out of one recording's transcript.
func voiceSamplesIn(ctx context.Context, r api.RecordingSimple) ([]voiceSample, error) {
	detail, err := client.GetDetail(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	url := detail.TranscriptURL()
	if url == "" {
		return nil, nil
	}
	data, err := client.FetchGzipped(ctx, url)
	if err != nil {
		return nil, err
	}
	segments, err := transcript.Parse(data)
	if err != nil {
		return nil, err
	}

	byName := map[string]*voiceSample{}
	for _, seg := range segments {
		name := strings.TrimSpace(seg.Speaker)
		if name == "" || genericLabel.MatchString(name) || seg.EndTime <= seg.StartTime {
			continue
		}
		sample, ok := byName[name]
		if !ok {
			sample = &voiceSample{name: name, recordingID: r.ID, recordingName: r.Name}
			byName[name] = sample
		}
		sample.ranges = append(sample.ranges, [2]int{int(seg.StartTime), int(seg.EndTime)})
		sample.speechMS += int(seg.EndTime - seg.StartTime)
	}

	var found []voiceSample
	for _, sample := range byName {
		if sample.speechMS >= enrollMinSpeech {
			found = append(found, *sample)
		}
	}
	return found, nil
}

// reconcileNames folds the spellings that turn out to be one person, asking
// before it merges anything.
func reconcileNames(samples map[string][]voiceSample, known []string) (map[string][]voiceSample, error) {
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return totalSpeech(samples[names[i]]) > totalSpeech(samples[names[j]]) })

	// A name already settled on is the one to merge toward, so known names and
	// the better-attested spellings are offered as targets first.
	targets := append([]string{}, known...)
	resolved := map[string][]voiceSample{}

	for _, name := range names {
		final := name
		if matches := speaker.Similar(name, targets); len(matches) > 0 {
			if matches[0].Same {
				final = matches[0].Name
			} else {
				choice, err := askMerge(name, samples[name], matches)
				if err != nil {
					return nil, err
				}
				final = choice
			}
		}
		resolved[final] = append(resolved[final], samples[name]...)
		if !contains(targets, final) {
			targets = append(targets, final)
		}
	}
	return resolved, nil
}

func askMerge(name string, samples []voiceSample, matches []speaker.Match) (string, error) {
	if enrollYes {
		fmt.Printf("  merging %q into %q\n", name, matches[0].Name)
		return matches[0].Name, nil
	}

	fmt.Fprintf(os.Stderr, "\n%q (%s of speech in %d recording(s)) resembles:\n",
		name, formatSpeech(totalSpeech(samples)), len(samples))
	for i, m := range matches {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, m.Name)
	}
	fmt.Fprintf(os.Stderr, "  n) a different person, keep %q\n", name)
	fmt.Fprintf(os.Stderr, "Which is it? [1] ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		// Nobody is at the keyboard, and merging two people on a default is
		// exactly the mistake this prompt exists to prevent.
		return "", fmt.Errorf("cannot ask which name is meant with no terminal attached — pass --yes to accept the closest match")
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

// selectSamples keeps the recordings where each person speaks most, then groups
// what is left by recording, since a recording is downloaded once for everyone
// enrolled from it.
func selectSamples(samples map[string][]voiceSample) (map[string][]voiceSample, map[string]int) {
	byRecording := map[string][]voiceSample{}
	chosen := map[string]int{}

	for name, found := range samples {
		sort.Slice(found, func(i, j int) bool { return found[i].speechMS > found[j].speechMS })
		if len(found) > enrollMaxPerSpeaker {
			found = found[:enrollMaxPerSpeaker]
		}
		chosen[name] = len(found)
		for _, s := range found {
			s.name = name
			byRecording[s.recordingID] = append(byRecording[s.recordingID], s)
		}
	}
	return byRecording, chosen
}

func reportPlan(samples map[string][]voiceSample, chosen map[string]int) {
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return totalSpeech(samples[names[i]]) > totalSpeech(samples[names[j]]) })

	recordings := map[string]bool{}
	for name, found := range samples {
		limit := chosen[name]
		sort.Slice(found, func(i, j int) bool { return found[i].speechMS > found[j].speechMS })
		for i := 0; i < limit && i < len(found); i++ {
			recordings[found[i].recordingID] = true
		}
	}

	fmt.Printf("\n%-30s %10s %12s %s\n", "SPEAKER", "SPEECH", "RECORDINGS", "USING")
	for _, name := range names {
		fmt.Printf("%-30s %10s %12d %5d\n",
			truncate(name, 30), formatSpeech(totalSpeech(samples[name])), len(samples[name]), chosen[name])
	}
	fmt.Printf("\n%d speaker(s) from %d recording(s).\n", len(names), len(recordings))
}

func enrollRecordings(ctx context.Context, whisper *modal.HTTPClient, byRecording map[string][]voiceSample) error {
	ids := make([]string, 0, len(byRecording))
	for id := range byRecording {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var enrolled, failed int
	for i, id := range ids {
		group := byRecording[id]
		names := make([]string, len(group))
		specs := make([]modal.SpeakerRanges, len(group))
		for j, s := range group {
			names[j] = s.name
			specs[j] = modal.SpeakerRanges{Name: s.name, Ranges: s.ranges}
		}

		fmt.Printf("\n[%d/%d] %s\n  %s\n", i+1, len(ids), truncate(group[0].recordingName, 70), strings.Join(names, ", "))

		tempURL, err := client.GetTempURL(ctx, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error getting audio URL: %v\n", err)
			failed++
			continue
		}
		audioData, err := client.FetchFile(ctx, tempURL, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error downloading audio: %v\n", err)
			failed++
			continue
		}

		result, err := whisper.EnrollSpeakers(ctx, audioData, specs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error enrolling: %v\n", err)
			failed++
			continue
		}
		for name := range result.Enrolled {
			fmt.Printf("  learned %s\n", name)
			enrolled++
		}
		for name, why := range result.Skipped {
			fmt.Printf("  skipped %s: %s\n", name, why)
		}
	}

	fmt.Printf("\n%d voice sample(s) learned", enrolled)
	if failed > 0 {
		fmt.Printf(", %d recording(s) failed", failed)
	}
	fmt.Println()
	return nil
}

func totalSpeech(samples []voiceSample) int {
	total := 0
	for _, s := range samples {
		total += s.speechMS
	}
	return total
}

func formatSpeech(ms int) string {
	seconds := ms / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func init() {
	speakerEnrollCmd.Flags().IntVar(&enrollMaxPerSpeaker, "max-per-speaker", 5, "how many recordings to learn each voice from")
	speakerEnrollCmd.Flags().IntVar(&enrollMinSpeech, "min-speech", 15000, "ignore a speaker who says less than this many ms in a recording")
	speakerEnrollCmd.Flags().BoolVar(&enrollDryRun, "dry-run", false, "show what would be learned without downloading audio")
	speakerEnrollCmd.Flags().BoolVar(&enrollYes, "yes", false, "merge every name that resembles a known one, without asking")
	speakerEnrollCmd.Flags().IntVar(&enrollWorkers, "workers", 6, "how many transcripts to read at once")
}
