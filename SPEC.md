# plaud-cli: modal-whisper Integration Spec

## Context

The `modal-whisper` service (deployed on Modal) has been updated with new transcription features. The CLI needs to expose these as flags on the `plaud transcribe` command and pass them through to the Modal function.

All processing happens server-side on Modal — the CLI is a thin client that passes parameters and receives a clean transcript.

## New Default Behavior

By default, `plaud transcribe` now enables **diarize + polish + compact** — the full pipeline. Users opt out of features instead of opting in.

## CLI Changes

### Flag design

Remove the existing `--diarize` flag. Replace with:

- `--options` (string) — comma-separated list of negative flags to disable features. Valid values: `no-diarize`, `no-polish`, `no-compact`
- `--context` (string) — path to a text/markdown file (meeting prep, agenda, notes). Read and sent as the `context_doc` kwarg. Optional — if omitted, polish still runs but without meeting context, and hotwords are not generated.
- `--compact-gap` (int, default: 2000) — max silence gap in ms before starting a new paragraph
- `--hotwords` (string) — manual override for hotwords, bypasses LLM-generated hotwords from context doc

### Logic

```
diarize  = "no-diarize" NOT in options     (default: true)
polish   = "no-polish"  NOT in options      (default: true)
compact  = "no-compact" NOT in options      (default: true)
```

- `compact` requires `diarize` — if diarize is off, compact is forced off too
- `--context` is read from disk and passed as `context_doc` kwarg
- If `--context` is provided, its contents are always sent (for hotword extraction even if polish is off)

### Files to change

`cmd/transcribe.go`:
- Remove `--diarize` flag
- Add `--options` (string), `--context` (string), `--compact-gap` (int)
- Parse `--options` into booleans for diarize/polish/compact
- Read `--context` file contents into a string
- Update help text and examples

`internal/modal/transcribe.go`:
- Update `TranscribeOpts` struct:
  - `Diarize bool` (keep, but default changes to true)
  - Add `Polish bool`
  - Add `Compact bool`
  - Add `CompactGap int`
  - Add `ContextDoc string`
- Pass all as kwargs to Modal function

### Examples

```
# Full pipeline (default) — diarize + polish + compact
plaud transcribe abc123

# With meeting context for better hotwords and polishing
plaud transcribe abc123 --context ./meeting-prep.md

# Disable polish (faster, raw transcript)
plaud transcribe abc123 --options no-polish

# Disable compact (keep sentence-level segments)
plaud transcribe abc123 --options no-compact --context ./prep.md

# Disable multiple features
plaud transcribe abc123 --options no-polish,no-compact

# Raw whisper output, no post-processing at all
plaud transcribe abc123 --options no-diarize,no-polish,no-compact

# Custom compact gap
plaud transcribe abc123 --compact-gap 3000 --context ./prep.md
```

### Modal kwargs mapping

| CLI state | Modal kwargs |
|---|---|
| default | `diarize=true, polish=true, compact=true, compact_gap=2000` |
| `--context ./f` | adds `context_doc="<file contents>"` |
| `--options no-polish` | `polish=false` |
| `--options no-diarize` | `diarize=false, compact=false` (compact forced off) |
| `--hotwords "X,Y"` | `hotwords="X,Y"` (overrides LLM-generated) |
