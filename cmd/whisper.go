package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/progress"
)

// whisperClient resolves the Modal credentials that transcription needs.
func whisperClient() (*modal.HTTPClient, error) {
	c := modal.LoadHTTPClient(cfg.ModalTokenID, cfg.ModalTokenSecret, cfg.ModalEndpointURL)
	if c == nil {
		return nil, fmt.Errorf("Modal not configured — run 'plaud modal-auth', or set MODAL_TOKEN_ID, MODAL_TOKEN_SECRET and MODAL_ENDPOINT_URL")
	}
	return c, nil
}

// whisperDefaults are the options for a transcription nobody configured, which
// is what sync and download run when Plaud holds no transcript of its own.
// Speaker recognition stays off: naming speakers needs a person at a browser,
// and these two commands run over a whole library unattended.
func whisperDefaults(language string) modal.TranscribeOpts {
	return modal.TranscribeOpts{
		Diarize:    true,
		Polish:     true,
		Compact:    true,
		CompactGap: 2000,
		Language:   language,
	}
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
			result = &modal.TranscribeResult{
				AudioID:  evt.AudioID,
				Segments: evt.Segments,
				Speakers: evt.Speakers,
			}

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
