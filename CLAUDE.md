# plaud-cli

CLI client for Plaud.ai — list, download, search, transcribe, and summarize voice recordings.

## Build & Run

```bash
go build -o plaud .
go run .
```

## Release

Releases are automated via GitHub Actions on tag push (`v*`). Uses goreleaser for cross-platform binaries.

```bash
git tag v0.x.x
git push --tags
```

## Architecture

- `cmd/` — Cobra CLI commands (one file per command)
- `internal/api/` — Plaud API HTTP client
- `internal/config/` — Config persistence (`~/.config/plaud/`)
- `internal/transcript/` — Transcript parsing, formatting (txt/srt/md), search, and filename utilities
- `internal/ai/` — Claude API integration for ask/summarize commands
- `internal/modal/` — Modal client for Whisper transcription
- `services/whisper/` — the Whisper service itself (Python), deployed to Modal

## External Services

- **Plaud API** (`api.plaud.ai`) — Recording data, transcripts, summaries
- **Modal** (`modal-whisper` app) — Whisper transcription with speaker diarization. Source: `services/whisper/`. Diarization uses WhisperX's `DiarizationPipeline` (from `whisperx.diarize`), not raw pyannote.
- **Anthropic Claude** — AI summaries and Q&A (`ANTHROPIC_API_KEY`)

## Segment Contract

Transcripts (from both Plaud API and Modal Whisper) use a shared segment format.

- **Schema definition:** `services/whisper/segment_schema.json` (JSON Schema)
- **Go struct:** `internal/transcript/transcript.go` — `Segment` struct
- Fields: `start_time` (ms), `end_time` (ms), `content` (string), `speaker` (string, empty if no diarization)

## Environment Variables

```
PLAUD_TOKEN            Access token, standing in for token.json entirely
PLAUD_DEVICE_ID        Device ID sent as x-device-id (derived from the token when unset)
PLAUD_PASSWORD         Password for `login --password`, instead of the prompt
PLAUD_API_URL          Override API endpoint
ANTHROPIC_API_KEY      Claude API key (ask/summarize commands)
MODAL_TOKEN_ID         Modal auth (or use `plaud modal-auth`)
MODAL_TOKEN_SECRET     Modal auth (or use `plaud modal-auth`)
MODAL_ENDPOINT_URL     Web endpoint of the deployed Whisper service
PLAUD_EMAIL            Non-interactive `login`: email to send the code to
PLAUD_CODE             Non-interactive `login`: the emailed code
PLAUD_OTP_TOKEN        Non-interactive `login`: the OTP token from the send step
```

`PLAUD_TOKEN` is what makes the CLI run where no interactive login ever happened: a container, a CI job, another person's machine. The environment wins over the file, and nothing is written to disk.

## Authentication

Three ways in, in `cmd/login.go`:

- **Password** (`login --password`) posts to `/auth/access-token`. The password is never sent in the clear: `GET /config/security` publishes a secp256k1 public key, and the password is sealed with ECIES before it goes out. `internal/api/crypto.go` implements that scheme and `crypto_test.go` pins it against a vector produced by eciesjs, the library the web client ships. A mismatch there is indistinguishable from a wrong password, so do not change the scheme without re-running those tests.
- **Email code** (`login`) is the two-step OTP flow, `/auth/otp-send-code` then `/auth/otp-login`. `--send-code` exposes the halves separately, printing the OTP handle so the code can be collected somewhere other than this terminal, and `--otp-token`/`--code` finish it. That is the flow to use when an assistant is running the commands on someone's behalf: a code expires and works once, whereas handing over a password does not.
- **Existing token** (`login --token`, or `PLAUD_TOKEN`).

Accounts created through Google, Apple or Microsoft SSO have no password until one is set in the Plaud app, so the password flow does not apply to them.

`/auth/access-token` also returns a `refresh_token` and expiry fields, which this client currently ignores. Access tokens last months and nothing here renews them.

## Transcription

Plaud issues no new transcripts for this account, so Whisper on Modal is the only thing here that turns audio into text. `transcribe` always goes to Whisper. `sync` and `download` prefer a transcript Plaud already holds and fall back to Whisper for the rest, which `--whisper=false` turns off.

`generate` asks Plaud to transcribe and needs the credits the account no longer has.

`sync` and `download` transcribe with speaker recognition on, which only matches against voices already learned and so needs nobody present. Teaching a new voice is the part that needs a person: `speaker name` for one, `speaker enroll` for a library's worth.

The GPU container is scaled to zero between jobs, so every run pays a cold start before its first stage reports, and `sync` says how many recordings it is about to send before the first one starts.

## Speaker Recognition

Diarization separates voices and calls them `SPEAKER_00`, `SPEAKER_01`. Recognition turns those into names, by comparing each voice against samples of people already learned.

Every Whisper transcription writes a `.speakers.json` beside it, holding the embedding of each label. That file is what lets a speaker be named from a transcript found on disk weeks later: the voice travels with the transcript, so naming needs no audio and nothing kept on the server.

- `speaker enroll` learns from the Plaud transcripts that already name their speakers, which is the one bulk source of labelled voice this account has. It reads every transcript, picks the recordings where each person speaks most, and sends only those stretches.
- `speaker name <transcript> <label> <name>` names one voice from a saved transcript and its speaker file.
- `speaker set` names one the server still holds an embedding for, and stops working once it no longer does.
- `speaker rename` moves the samples of one spelling onto another; `speaker forget` drops a voice learned from the wrong person, which otherwise keeps claiming somebody else's in every transcription that follows.

The store holds full names only. A lone first name identifies whichever Amanda the person typing it had in mind, and the store is shared with everyone using the service, so `speaker name` and `speaker rename` refuse anything shorter and `speaker list` marks the ones already stored that way.

Transcripts call people whatever the person typing felt like, and only somebody who knows them can say who "luca" or "Vic" is. `~/.config/plaud/speaker-names.json` maps those spellings to full names; `speaker enroll` reads it, leaves out whoever is still unresolved, and writes the outstanding ones back into that file to be filled in.

A new name is also checked against the known ones before being created. Two spellings of one person are two people to everything mechanical, and the samples they split can only be rejoined by `speaker rename`.

Enrollment embeds with the model the diarization pipeline itself uses (`services/whisper/modal_whisper/embed.py`). Under any other model, enrolled and diarized voices land in different spaces, where nothing ever matches and nothing ever reports an error.

The voices live in a SQLite file on a Modal volume, and a container serves whatever view of that volume it last loaded. Writing without reloading first publishes the database as it looked before someone else's write, undoing it while reporting success. Every path that touches a voice goes through `open_speaker_store()` for that reason.

## Config Files

All stored in `~/.config/plaud/` with 0600 permissions:
- `token.json` — Auth token, device ID, Modal credentials
- `sync-state.json` — Incremental sync tracking
- `update-state.json` — Version check cache
- `cache/transcripts/` — Local transcript cache

A transcript's speakers live beside the transcript, not here: `transcript.md` is accompanied by `transcript.speakers.json`.
