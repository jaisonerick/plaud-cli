package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/progress"
	"github.com/spf13/cobra"
)

var transcribeDemoCmd = &cobra.Command{
	Use:    "transcribe-demo",
	Short:  "Demo the transcription progress UI with mock data",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Client-side stages
		tracker := progress.NewTracker(os.Stderr, []progress.StageDef{
			{ID: "download", Label: "Downloading audio"},
			{ID: "connect", Label: "Waiting for server"},
			{ID: "upload", Label: "Uploading audio"},
		})

		// 1. Download audio — simulate bytes arriving
		totalBytes := 2_400_000
		tracker.Update(progress.Event{Stage: "download", Status: "started"})
		downloaded := 0
		for downloaded < totalBytes {
			chunk := 120_000
			if downloaded+chunk > totalBytes {
				chunk = totalBytes - downloaded
			}
			downloaded += chunk
			pct := downloaded * 100 / totalBytes
			tracker.Update(progress.Event{
				Stage:  "download",
				Status: "progress",
				Detail: fmt.Sprintf("%d%%  %.1f MB", pct, float64(downloaded)/1_000_000),
			})
			time.Sleep(40 * time.Millisecond)
		}
		tracker.Update(progress.Event{Stage: "download", Status: "done", Detail: "2.4 MB"})

		// 2. Connect — simulate cold start
		tracker.Update(progress.Event{Stage: "connect", Status: "started"})
		time.Sleep(2 * time.Second)
		tracker.Update(progress.Event{Stage: "connect", Status: "done"})

		// 3. Upload — simulate upload progress
		tracker.Update(progress.Event{Stage: "upload", Status: "started", Detail: "2.4 MB"})
		for i := 1; i <= 10; i++ {
			time.Sleep(80 * time.Millisecond)
			pct := i * 10
			tracker.Update(progress.Event{
				Stage:  "upload",
				Status: "progress",
				Detail: fmt.Sprintf("%d%%  2.4 MB", pct),
			})
		}
		tracker.Update(progress.Event{Stage: "upload", Status: "done", Detail: "2.4 MB"})

		// 4. Server init — add server stages dynamically
		tracker.AddStages([]progress.StageDef{
			{ID: "context", Label: "Extracting context"},
			{ID: "transcribe", Label: "Transcribing audio"},
			{ID: "align", Label: "Aligning timestamps"},
			{ID: "diarize", Label: "Diarizing speakers"},
			{ID: "speaker_assign", Label: "Assigning speakers"},
			{ID: "segment_convert", Label: "Converting segments"},
			{ID: "speaker_recognition", Label: "Recognizing speakers"},
			{ID: "polish", Label: "Polishing transcript"},
			{ID: "compact", Label: "Compacting segments"},
		})
		tracker.AddStages([]progress.StageDef{
			{ID: "save", Label: "Saving transcript"},
		})

		// 5. Context extraction
		tracker.Update(progress.Event{Stage: "context", Status: "started"})
		time.Sleep(1500 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "context", Status: "done", Detail: "5 hotwords"})

		// 6. Transcription — show segments being found
		tracker.Update(progress.Event{Stage: "transcribe", Status: "started"})
		segments := 0
		for i := 0; i < 15; i++ {
			segments += 8 + i
			tracker.Update(progress.Event{
				Stage:  "transcribe",
				Status: "progress",
				Detail: fmt.Sprintf("%d segments", segments),
			})
			time.Sleep(200 * time.Millisecond)
		}
		tracker.Update(progress.Event{Stage: "transcribe", Status: "done", Detail: fmt.Sprintf("%d segments", segments)})

		// 7. Alignment
		tracker.Update(progress.Event{Stage: "align", Status: "started"})
		time.Sleep(1500 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "align", Status: "done"})

		// 8. Diarization
		tracker.Update(progress.Event{Stage: "diarize", Status: "started"})
		time.Sleep(2000 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "diarize", Status: "done", Detail: "3 speakers"})

		// 9. Speaker assignment
		tracker.Update(progress.Event{Stage: "speaker_assign", Status: "started"})
		time.Sleep(200 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "speaker_assign", Status: "done"})

		// 10. Segment conversion
		tracker.Update(progress.Event{Stage: "segment_convert", Status: "started"})
		time.Sleep(100 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "segment_convert", Status: "done", Detail: fmt.Sprintf("%d segments", segments)})

		// 11. Speaker recognition
		tracker.Update(progress.Event{Stage: "speaker_recognition", Status: "started"})
		time.Sleep(300 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "speaker_recognition", Status: "done", Detail: "2 matched"})

		// 12. Polishing — show chunk progress
		totalChunks := 8
		tracker.Update(progress.Event{Stage: "polish", Status: "started", Detail: fmt.Sprintf("0/%d chunks", totalChunks)})
		for i := 1; i <= totalChunks; i++ {
			time.Sleep(400 * time.Millisecond)
			tracker.Update(progress.Event{
				Stage:  "polish",
				Status: "progress",
				Detail: fmt.Sprintf("%d/%d chunks", i, totalChunks),
			})
		}
		tracker.Update(progress.Event{Stage: "polish", Status: "done", Detail: fmt.Sprintf("%d chunks", totalChunks)})

		// 13. Compaction
		tracker.Update(progress.Event{Stage: "compact", Status: "started"})
		time.Sleep(150 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "compact", Status: "done", Detail: "47 paragraphs"})

		// 14. Save
		tracker.Update(progress.Event{Stage: "save", Status: "started"})
		time.Sleep(100 * time.Millisecond)
		tracker.Update(progress.Event{Stage: "save", Status: "done"})

		tracker.Wait()

		fmt.Println("\nTranscript saved to ./meeting_2026-03-27_whisper.md")
		fmt.Println("Audio ID: abc123-def456")
		fmt.Println("Speakers: Alice, Bob, SPEAKER_02")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(transcribeDemoCmd)
}
