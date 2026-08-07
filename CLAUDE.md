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

## External Services

- **Plaud API** (`api.plaud.ai`) — Recording data, transcripts, summaries
- **Modal** (`modal-whisper` app) — Whisper transcription with speaker diarization. Source: `~/code/jaisonerick/modal-whisper`. Diarization uses WhisperX's `DiarizationPipeline` (from `whisperx.diarize`), not raw pyannote.
- **Anthropic Claude** — AI summaries and Q&A (`ANTHROPIC_API_KEY`)

## Segment Contract

Transcripts (from both Plaud API and Modal Whisper) use a shared segment format.

- **Schema definition:** `modal-whisper/segment_schema.json` (JSON Schema)
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

## Config Files

All stored in `~/.config/plaud/` with 0600 permissions:
- `token.json` — Auth token, device ID, Modal credentials
- `sync-state.json` — Incremental sync tracking
- `update-state.json` — Version check cache
- `cache/transcripts/` — Local transcript cache
