package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	genLang     string
	genSpeaker  bool
	genSummType string
	genReload   bool
	genWait     bool
	genTimeout  time.Duration
	genInterval time.Duration
)

var generateCmd = &cobra.Command{
	Use:     "generate <id> [<id>...]",
	Aliases: []string{"gen"},
	Short:   "Start transcription for one or more recordings (remote)",
	Long: `Trigger Plaud-side transcription for recordings that were synced from the
device but not transcribed yet — the equivalent of clicking "Generate" in the
web app. This consumes your Plaud transcription quota.

Plaud's remote trigger always produces a transcript together with an AI summary
(its "Auto generation" flow, which picks the best summary template for you);
there is no transcript-only variant. Pass --summary-template <id> to force a
specific template instead. Language defaults to auto-detect; use --lang pt /
--lang en to force it. Speakers are separated by default (--speaker=false to
disable).

The work runs asynchronously on Plaud's side. Use --wait to poll until each
transcript is ready.

Examples:
  plaud generate abc123                       # transcript + auto summary (auto language)
  plaud generate abc123 --lang pt             # force Portuguese
  plaud generate abc123 --speaker=false       # don't separate speakers
  plaud generate abc123 --wait                # block until the transcript is ready
  plaud generate abc123 def456 ghi789         # activate several at once
  plaud generate abc123 --reload              # re-transcribe (already has a transcript)`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Show remaining quota up front (best-effort).
		if st, err := client.GetTransStatus(ctx); err == nil {
			fmt.Printf("Transcription quota remaining: %s\n\n", api.FormatDurationMs(int64(st.RemainTotal*1000)))
		}

		opts := api.TranscribeOptions{
			Language:    genLang,
			Diarization: genSpeaker,
			Reload:      genReload,
			SummType:    genSummType,
		}

		var failures int
		for _, id := range args {
			if err := generateOne(ctx, id, opts); err != nil {
				fmt.Printf("  ✗ %s: %v\n", id, err)
				failures++
			}
		}
		if failures > 0 {
			return fmt.Errorf("%d of %d recording(s) failed", failures, len(args))
		}
		return nil
	},
}

func generateOne(ctx context.Context, id string, opts api.TranscribeOptions) error {
	detail, err := client.GetDetail(ctx, id)
	if err != nil {
		return fmt.Errorf("fetching details: %w", err)
	}

	label := detail.Name
	if label == "" {
		label = id
	}
	fmt.Printf("● %s\n", label)

	if detail.HasTranscript() && !opts.Reload {
		fmt.Printf("  already transcribed — skipping (use --reload to re-transcribe)\n")
		return nil
	}

	if _, err := client.StartTranscription(ctx, id, opts); err != nil {
		return err
	}
	fmt.Printf("  transcription + summary started\n")

	if !genWait {
		return nil
	}
	return waitForTranscript(ctx, id)
}

func waitForTranscript(ctx context.Context, id string) error {
	start := time.Now()
	deadline := start.Add(genTimeout)
	for {
		time.Sleep(genInterval)
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("polling: %w", err)
		}
		elapsed := time.Since(start).Round(time.Second)
		if detail.HasTranscript() {
			fmt.Printf("  ✓ transcript ready after %s\n", elapsed)
			return nil
		}
		fmt.Printf("  … transcribing (%s elapsed)\n", elapsed)
		if time.Now().After(deadline) {
			return fmt.Errorf("still not ready after %s (it may finish later; check `plaud info %s`)", genTimeout, id)
		}
	}
}

func init() {
	generateCmd.Flags().StringVar(&genLang, "lang", "auto", "transcription language: auto, pt, en, ... (auto-detects)")
	generateCmd.Flags().BoolVar(&genSpeaker, "speaker", true, "separate speakers (diarization)")
	generateCmd.Flags().StringVar(&genSummType, "summary-template", "", "summary template id (default: Plaud auto-selects)")
	generateCmd.Flags().BoolVar(&genReload, "reload", false, "re-transcribe even if a transcript already exists")
	generateCmd.Flags().BoolVar(&genWait, "wait", false, "poll until the transcript is ready")
	generateCmd.Flags().DurationVar(&genTimeout, "timeout", 20*time.Minute, "max time to wait with --wait")
	generateCmd.Flags().DurationVar(&genInterval, "poll-interval", 8*time.Second, "poll interval with --wait")
	rootCmd.AddCommand(generateCmd)
}
