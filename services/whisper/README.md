# whisper

Speech-to-text for `plaud transcribe`, built on [WhisperX](https://github.com/m-bain/whisperX) (Whisper large-v3) and deployed to [Modal](https://modal.com) as the `modal-whisper` app.

The container holds an A10G GPU and is scaled to zero between jobs, so it is billed for the time a transcription actually runs plus the idle window that follows. Every request that arrives at an idle app pays a cold start.

## What it exposes

A FastAPI app behind Modal proxy auth. Callers send `Modal-Key` and `Modal-Secret` headers, which are workspace proxy tokens rather than the account tokens that deploy the app.

| Route | Purpose |
| --- | --- |
| `POST /transcribe` | Multipart `audio` plus an `options` JSON field. `?stream=true` returns the pipeline's progress as server-sent events, ending in a `result` event. |
| `PUT /speakers/{audio_id}/{speaker_id}` | Name a diarized speaker the store still holds, from a transcription it has not yet forgotten. |
| `POST /speakers` | Name a voice from an embedding the caller supplies, which is how a transcript already on disk names its speakers. |
| `POST /speakers/enroll` | Learn voices from a recording someone already attributed: multipart `audio` plus `speakers`, `[{"name": str, "ranges": [[start_ms, end_ms], ...]}]`. |
| `PATCH /speakers` | Move every sample of one name onto another, which is how two spellings of one person are rejoined. |
| `DELETE /speakers` | Drop every sample of a name, for a voice learned from the wrong person. |
| `GET /speakers` | The voices known, each with how many samples back it. |

`options` accepts `language`, `context_doc`, `diarize`, `speaker_recognition`, `speaker_threshold`, `polish`, `compact` and `compact_gap`. `internal/modal/http.go` is the Go client for all three routes, and `modal_whisper/builder.py` runs the stages.

Segments follow `segment_schema.json`, shared with the Go `Segment` struct. A diarized result also carries `embeddings`, one 256-dimension vector per speaker label, which is what lets the caller name a voice long after the recording was processed.

Enrollment embeds through `modal_whisper/embed.py`, pinned to the model the diarization pipeline uses internally. Enrolling under any other model places those vectors in a space the diarized ones are never compared against successfully, and nothing about that failure is visible except that recognition stops working.

## Deploy

Pushing to `main` deploys, whenever the push touches this directory. The workflow is `.github/workflows/deploy-whisper.yml` and it authenticates with the `MODAL_TOKEN_ID` and `MODAL_TOKEN_SECRET` repository secrets.

To deploy by hand:

```bash
modal deploy app.py
```

## Setting up a fresh workspace

The app reads two Modal secrets, and fails at container start when either is absent:

```bash
modal secret create huggingface-secret HF_TOKEN=hf_...
modal secret create openrouter-secret OPENROUTER_API_KEY=sk-or-...
```

`HF_TOKEN` drives diarization, which additionally needs the licences for [pyannote/segmentation-3.0](https://huggingface.co/pyannote/segmentation-3.0) and [pyannote/speaker-diarization-3.1](https://huggingface.co/pyannote/speaker-diarization-3.1) accepted on that account. `OPENROUTER_API_KEY` drives the polishing and context-extraction stages.

One Modal volume, `whisper-model-cache`, carries state across cold starts: it mounts at `/cache` and holds both the downloaded model weights and the speaker database at `/cache/speakers.db`.

Point the CLI at the deployed endpoint with `plaud modal-auth`, or with `MODAL_ENDPOINT_URL`.
