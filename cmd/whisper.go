package cmd

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/auth"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/progress"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

// whisperClient builds a client that signs each request with the Google
// account this machine signed in as. The token is fetched once per run and
// held here, so a command making several calls signs in once.
func whisperClient() (*modal.HTTPClient, error) {
	session, err := auth.LoadSession()
	if err != nil {
		return nil, err
	}
	if session == nil || session.RefreshToken == "" {
		return nil, fmt.Errorf("not signed in — run 'plaud auth login'")
	}

	var cached string
	token := func(ctx context.Context) (string, error) {
		if cached != "" {
			return cached, nil
		}
		config, err := auth.FetchConfig(ctx, whisperEndpoint())
		if err != nil {
			return "", err
		}
		cached, err = auth.IDToken(ctx, config, session)
		return cached, err
	}

	return modal.NewHTTPClient(cfg.WhisperURL, token), nil
}

// whisperTranscribe downloads a recording's audio and transcribes it on Modal,
// drawing the progress of both onto w. It returns the audio next to the result
// because speaker identification replays it locally.
func whisperTranscribe(ctx context.Context, w io.Writer, whisper *modal.HTTPClient, id string, opts modal.TranscribeOpts) (*modal.TranscribeResult, []byte, error) {
	tracker := progress.NewTracker(w, []progress.StageDef{
		{ID: "download", Label: "Downloading audio"},
		{ID: "upload", Label: "Waiting for server"},
	})

	fail := func(err error) (*modal.TranscribeResult, []byte, error) {
		tracker.Abort()
		tracker.Wait()
		return nil, nil, err
	}

	tracker.Update(progress.Event{Stage: "download", Status: "started"})
	tempURL, err := client.GetTempURL(ctx, id)
	if err != nil {
		return fail(fmt.Errorf("getting download URL: %w", err))
	}

	audioData, err := client.FetchFile(ctx, tempURL, func(received, total int64) {
		if total > 0 {
			tracker.Update(progress.Event{
				Stage:  "download",
				Status: "progress",
				Detail: fmt.Sprintf("%d%%  %.1f MB", received*100/total, float64(received)/1e6),
			})
		}
	})
	if err != nil {
		return fail(fmt.Errorf("downloading audio: %w", err))
	}
	tracker.Update(progress.Event{Stage: "download", Status: "done", Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})

	// One stage covers upload, cold start and handshake: the container is
	// scaled to zero between jobs, so the first server event is the only
	// moment we can prove it woke up.
	tracker.Update(progress.Event{Stage: "upload", Status: "started"})

	opts.RecordingID = id
	events, errCh := whisper.TranscribeStream(ctx, audioData, opts, modal.StreamCallbacks{})

	var result *modal.TranscribeResult
	for evt := range events {
		switch evt.Type {
		case "init":
			tracker.Update(progress.Event{Stage: "upload", Status: "done"})
			tracker.AddStages(evt.Stages)

		case "update":
			e := progress.Event{Stage: evt.Stage, Status: evt.Status}
			if evt.Detail != nil {
				e.Detail = *evt.Detail
			}
			if evt.Progress != nil {
				e.Current = evt.Progress.Current
				e.Total = evt.Progress.Total
			}
			tracker.Update(e)

		case "result":
			result = evt.Result()

		case "error":
			return fail(fmt.Errorf("transcription failed at %s: %s", evt.Stage, evt.Message))
		}
	}
	if err := <-errCh; err != nil {
		return fail(err)
	}
	if result == nil {
		return fail(fmt.Errorf("no result received — the server stream ended prematurely (the container may have crashed)"))
	}

	// The server declares its stages up front but drops some of them when
	// diarization fails, taking compaction and speaker recognition with it.
	// Wait blocks until every bar finishes, so retire the ones that never
	// reported rather than hang a sync that nobody is watching.
	tracker.Abort()
	tracker.Wait()
	return result, audioData, nil
}

// reportLanguage names the language when the audio chose it. Whisper
// translates rather than mis-spells when it chooses wrong, so the transcript
// itself is fluent and says nothing about the decision.
func reportLanguage(w io.Writer, result *modal.TranscribeResult) {
	lang := result.Language
	if !lang.Detected {
		return
	}
	agreed := int(math.Round(lang.Agreement * float64(lang.Samples)))
	if lang.Agreement < 0.6 {
		fmt.Fprintf(w, "Warning: language was detected as %s, but only %d of %d samples of the audio agreed. Pass --language to settle it.\n", lang.Code, agreed, lang.Samples)
		return
	}
	fmt.Fprintf(w, "Language detected as %s (%d of %d samples)\n", lang.Code, agreed, lang.Samples)
}

// reportSparse says when a transcript came back holding far less speech than
// the audio it covers, which is what a decode that collapsed looks like from
// the outside: a shorter meeting, not an error.
func reportSparse(w io.Writer, result *modal.TranscribeResult) {
	if !result.Sparse {
		return
	}
	fmt.Fprintf(w, "Warning: this transcript carries %.1f characters per second of speech, far below what continuous speech produces. Check it before using it.\n", result.CharsPerSecond)
}

// reportUnpolished names the segments left as the recogniser wrote them, and
// which of the two reasons applies. A correction the guard would not take is
// the guard doing its job; a chunk that answered nothing twice is a fault, and
// reading them as the same number is what hid the second one.
func reportUnpolished(w io.Writer, result *modal.TranscribeResult) {
	if result.Refused > 0 {
		fmt.Fprintf(w, "Polishing refused %s, which stand as transcribed\n", segmentCount(result.Refused))
	}
	if result.Unanswered > 0 {
		fmt.Fprintf(w, "Polishing never came back for %s, even on a second ask. They stand as transcribed, and the wording they would have had is missing.\n", segmentCount(result.Unanswered))
	}
}

func segmentCount(n int) string {
	if n == 1 {
		return "1 segment"
	}
	return fmt.Sprintf("%d segments", n)
}

// saveWhisperTranscript writes a transcript, and in markdown the ids of the
// voices in it. The voices themselves stay on the server; what the file keeps
// is which one each name stands for, so a name settled later can replace it.
func saveWhisperTranscript(result *modal.TranscribeResult, format, dest string) error {
	if format != "md" || len(result.Voices) == 0 {
		return saveTranscript(result.Segments, format, dest)
	}

	_, content := transcript.Format(result.Segments, format)
	return os.WriteFile(dest, []byte(transcript.WriteVoices(content, voiceBlockOf(result))), 0644)
}

// voiceBlockOf pairs each name the transcript writes with the voices behind it.
func voiceBlockOf(result *modal.TranscribeResult) transcript.VoiceBlock {
	block := transcript.VoiceBlock{}
	for label, id := range result.Voices {
		name := result.Speakers[label]
		if name == "" {
			name = label
		}
		block[name] = append(block[name], id)
	}
	return block
}
