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
- **Modal** (`modal-whisper` app) — Whisper transcription with speaker diarization. Source: `~/code/jaisonerick/modal-whisper`
- **Anthropic Claude** — AI summaries and Q&A (`ANTHROPIC_API_KEY`)

## Segment Contract

Transcripts (from both Plaud API and Modal Whisper) use a shared segment format.

- **Schema definition:** `modal-whisper/segment_schema.json` (JSON Schema)
- **Go struct:** `internal/transcript/transcript.go` — `Segment` struct
- Fields: `start_time` (ms), `end_time` (ms), `content` (string), `speaker` (string, empty if no diarization)

## Environment Variables

```
PLAUD_API_URL          Override API endpoint
ANTHROPIC_API_KEY      Claude API key (ask/summarize commands)
MODAL_TOKEN_ID         Modal auth (or use `plaud modal-auth`)
MODAL_TOKEN_SECRET     Modal auth (or use `plaud modal-auth`)
```

## Config Files

All stored in `~/.config/plaud/` with 0600 permissions:
- `token.json` — Auth token, device ID, Modal credentials
- `sync-state.json` — Incremental sync tracking
- `update-state.json` — Version check cache
- `cache/transcripts/` — Local transcript cache
