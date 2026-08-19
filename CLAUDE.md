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
PLAUD_WHISPER_URL      Override the transcription service endpoint
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

## Using the Transcription Service

The service is shared, and a Google account is the whole of what gets a person in: no account on the cloud it runs on, no keys to hand over. `plaud auth login` opens a browser once and keeps a refresh token; every request carries a Google identity token, which the service verifies before it does anything.

Only accounts on the domains in `services/whisper/modal_whisper/auth.py` are served. That list is the entire defence, because the endpoint itself answers anyone who knows the URL: it runs a GPU somebody pays for and writes into a store everyone shares.

`GET /auth/config` is the one route outside the guest list, since a caller needs it before it can have a token. Every other route hangs off a router that carries the check, so a route added later cannot forget it.

## Transcription

Plaud issues no new transcripts for this account, so Whisper on Modal is the only thing here that turns audio into text. `transcribe` always goes to Whisper. `sync` and `download` prefer a transcript Plaud already holds and fall back to Whisper for the rest, which `--whisper=false` turns off.

`generate` asks Plaud to transcribe and needs the credits the account no longer has.

`sync` and `download` transcribe with speaker recognition on, which only matches against voices already learned and so needs nobody present. Teaching a new voice is the part that needs a person: `speaker name` for one, `speaker enroll` for a library's worth.

The GPU container is scaled to zero between jobs, so every run pays a cold start before its first stage reports, and `sync` says how many recordings it is about to send before the first one starts.

## Speaker Recognition

Diarization separates voices and calls them `SPEAKER_00`, `SPEAKER_01`. Recognition turns those into people, by comparing each voice against ones already learned.

A person is a **first name, a last name and a company**, and is written everywhere as `First Last (Company)` — which is the form transcripts carry. Anything past the second word of a name is dropped: what used to arrive there was a company glued on, and it has a field of its own now. Every person also records **who added them**, the Google account that did it.

The store is shared by everyone signed in, which is why a lone first name is refused: "Amanda" names whichever Amanda the person typing had in mind. `people.folded` is UNIQUE, so a second spelling of one person cannot be created at all — every previous guard against that was a convention, and conventions are what split them.

No voice ever leaves the service. An embedding that travelled would put people who never agreed to it on the laptop of everyone who transcribed a meeting they were in. What makes naming possible later is that a transcription's voices are keyed by the **Plaud recording id**, which the caller already knows and never has to write down.

- `speaker name <recording-id> <label> "First Last" --company X` says who one of the voices is.
- `speaker alias "<spelling>" "First Last"` records that transcripts spelling somebody "luca" or "Vic" mean that person. The answer lives on the service, so it is given once and everybody enrolling afterwards benefits.
- `speaker enroll` learns from the Plaud transcripts that already name their speakers, resolving each spelling through those aliases and leaving out whoever is still unresolved.
- `speaker rename` corrects who somebody is, carrying their voices; `speaker forget` drops a person learned wrongly, which otherwise keeps claiming somebody else's voice in every transcription.

Enrollment embeds with the model the diarization pipeline itself uses (`services/whisper/modal_whisper/embed.py`). Under any other model, enrolled and diarized voices land in different spaces, where nothing ever matches and nothing ever reports an error.

`fold` exists twice, in `internal/speaker/names.go` and `services/whisper/modal_whisper/speaker_store.py`, and the two must agree: one decides what name to offer, the other what is stored, and a disagreement is a person who cannot be found under the name they were saved with. Both are tested against the same cases.

The voices live in a SQLite file on a Modal volume, and a container serves whatever view of that volume it last loaded. Writing without reloading first publishes the database as it looked before someone else's write, undoing it while reporting success. Every path that touches a voice goes through `open_speaker_store()` for that reason.

## Config Files

All stored in `~/.config/plaud/` with 0600 permissions:
- `token.json` — Plaud auth token and device ID
- `auth.json` — the Google sign-in for the transcription service (0600; a refresh token is worth the account)
- `sync-state.json` — Incremental sync tracking
- `update-state.json` — Version check cache
- `cache/transcripts/` — Local transcript cache

