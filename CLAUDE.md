# plaud-cli

CLI client for Plaud.ai — list, transcribe, download, search and summarize voice recordings.

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

`transcript` is the one way to get the text of a recording. It reuses a transcript already on record and transcribes the audio when there is none, and `--force` transcribes again over one that is there, which is what an old transcript deserves. `download` only ever copies a file that exists, which leaves it the audio and the summary.

Which engine ran is not a choice a caller makes, and the CLI does not offer one. Neither are the stages: no flag turns off diarization, polishing, recognition or compaction. The tool exists to finish a transcript, and a caller weighing whether to run one of those is being asked to price the tool rather than use it.

One of `--context` or `--context-file` is required: the description written out, or a file holding it. It is what settles how the names in it are spelt; without it the polisher guesses, and it guesses differently on each run, so two transcripts of the same people disagree. Which of the two it is used to be guessed from the value, and that guess was wrong in both directions: a description in Portuguese carries a date, a date carries a slash, and a slash read as a path turned the sentence into a filename nobody could open. `--context` now refuses a value that names a file that exists, rather than polishing a transcript against a path.

A context that does not name the people and companies in the recording is worse than a short one that does: the polisher writes a company it half-heard into a name it recognises from what it was given, so a briefing about other clients turns a real name into theirs.

Both take one recording by id, or every recording a filter keeps. `download` skips what is already on disk unless `--force` says otherwise. The output directory is the record of what has been done, so there is no state file to go stale. `internal/transcript` names the file, and the same name is what makes the skip work.

`transcript` does not skip a file it finds: it settles again who the voices in it are. A voice named after a transcript was written leaves that transcript calling somebody SPEAKER_03 forever, and nothing else would fix it, since the text lives only in that file.

**A name is a rendering; the id is the record.** A markdown transcript carries, in its front matter, which voice each name in it stands for: `"Jaison Erick (NexaEdge)": [v_7f3a91]`. `POST /speakers/{id}/whois` takes those ids, finds the embedding each one was stored with and answers who it is against the people known today. No audio is fetched and nothing is decoded. What it costs is one request.

The id is what makes this exact. A label is one run's numbering, and transcribing again renumbers it, so the SPEAKER_03 of an old file and the SPEAKER_03 in the store are the same string and not the same voice. An id is never handed out twice: a transcript whose recording was separated again asks about voices that no longer exist and is told so, instead of being answered with whoever holds that number now. A file written before the ids has only its labels to ask by, and that answers while the recording has not been transcribed again.

One name can hold more than one voice, since diarization splits a person as readily as it merges two. When the voices under one name disagree about who they are, the name stands and the run says so: two voices under one name are either a person who was split, which agrees, or a name given to the wrong voice, which is for a person to settle.

Only markdown is refreshed, and the rewrite replaces the name on the turn's own line, leaving every other byte alone: a filed transcript gains front matter and corrections after it is written, and rendering it again would drop them.

Nothing the caller knows about a recording reaches the decoder. Whisper reads a prompt as the transcript so far and carries on writing it, and the batched pipeline hands that same prompt to every window of the audio, so a term supplied up front becomes a term the recording contains. `--context` is read at the other end, in polishing, where a wrong guess costs a word instead of the speech around it. A decode that collapses returns less text for the same speech rather than an error, so the pipeline measures characters per second of speech and the CLI says so when a transcript lands far below it.

A recording that holds no speech is refused rather than transcribed. The batched decoder never applies the fallback the sequential one does, so a window the voice detector kept by mistake comes back as a confident sentence: six seconds of a microphone being put down decoded as "Thank you." at -0.69, where speech sits around -0.1. `services/whisper/modal_whisper/speech.py` drops a window below the decoder's own floor and, for a recording holding almost no speech at all, refuses the lot. Nothing is written and no voice is stored, which matters more than the text: a voice print of room tone is nameable, and would then claim somebody in every transcription. A meeting is never judged this way, because its hard passages are hard rather than imagined.

A recording with no `--language` has its language voted on by samples taken across the whole of it, and the vote reaches the CLI. Whisper translates rather than mis-spells when it guesses wrong, so a meeting held in one language comes back fluent and entire in another, with nothing in the file to say a decision was made, and the opening thirty seconds of a meeting are people arriving.

Polishing is an LLM pass over a text that is read afterwards as a quotation, and nothing in a request for spelling and punctuation stops a model returning a summary, half a sentence, or a line of its own training data. `services/whisper/modal_whisper/polish_guard.py` judges every corrected segment against what was transcribed, and a segment it refuses stands as transcribed. A chunk whose answer carries no segments at all is a failed call rather than a verdict on the speech, so it is asked once more before its segments are left alone. The two reach the polish progress line and the CLI as separate counts, because a run whose corrections were thrown away otherwise looks exactly like one that needed none, and a stretch of a meeting nobody polished looks exactly like a correction the guard declined.

Speaker recognition only matches against voices already learned, and so needs nobody present. Teaching a new voice is the part that needs a person: `speaker name` for one, `speaker enroll` for a library's worth.

The GPU container is scaled to zero between jobs, so every run pays a cold start before its first stage reports, and `transcript` says how many recordings it is about to send before the first one starts.

## Speaker Recognition

Diarization separates voices and calls them `SPEAKER_00`, `SPEAKER_01`. Recognition turns those into people, by comparing each voice against ones already learned.

A person is a **first name, a surname and a company**, and is written everywhere as `First Last (Company)` — which is the form transcripts carry. The surname is everything after the first name, kept whole: "da Silva" and "La O" are surnames, not leftovers to trim. Every person also records **who added them**, the Google account that did it.

A surname may be genuinely unknown, which is not the same as one nobody bothered to type. Saying so takes `--surname-unknown`, so the default still catches the "Amanda" typed without thinking; the company then does the identifying, and a second person of that first name is refused until somebody looks a surname up.

The store is shared by everyone signed in, which is why a lone first name is refused: "Amanda" names whichever Amanda the person typing had in mind. `people.folded` is UNIQUE, so a second spelling of one person cannot be created at all — every previous guard against that was a convention, and conventions are what split them.

No voice ever leaves the service. An embedding that travelled would put people who never agreed to it on the laptop of everyone who transcribed a meeting they were in. What makes naming possible later is that a transcription's voices are keyed by the **Plaud recording id**, which the caller already knows and never has to write down. The service refuses a transcription that does not carry one, and transcribing a recording again replaces its voices rather than joining them: a label left from a run that found more speakers points at a voice that is no longer there, and naming it would put a person on somebody else's voice.

- `speaker name <recording-id> <label> "First Last" --company X` says who one of the voices is.
- `speaker teach <recording-id> --ranges file.json` learns from stretches somebody chose, one sample per person. A label that holds two voices is the average of both, so naming it whole teaches neither, and that speech is lost to the recogniser until it is cut apart.
- `speaker enroll` learns from the Plaud transcripts that name their speakers in full, and leaves out whoever they name by a first name alone. Nothing mechanical turns "Tom" into Antonio Colombo, and there is no longer a dictionary that would: one person has one name. Whoever is left out is named from a recording instead, and is recognised by voice from then on however the transcript spelt them.
- `speaker rename` corrects who somebody is, carrying their voices; `speaker forget` drops a person learned wrongly, which otherwise keeps claiming somebody else's voice in every transcription.

Enrollment embeds with the model the diarization pipeline itself uses (`services/whisper/modal_whisper/embed.py`). Under any other model, enrolled and diarized voices land in different spaces, where nothing ever matches and nothing ever reports an error.

`fold` exists twice, in `internal/speaker/names.go` and `services/whisper/modal_whisper/speaker_store.py`, and the two must agree: one decides what name to offer, the other what is stored, and a disagreement is a person who cannot be found under the name they were saved with. Both are tested against the same cases.

The voices live in a SQLite file on a Modal volume, and a container serves whatever view of that volume it last loaded. Writing without reloading first publishes the database as it looked before someone else's write, undoing it while reporting success. Every path that touches a voice goes through `open_speaker_store()` for that reason. A write that is never committed does not get that far at all: it stays in the container that made it, so the voices a transcription separated are gone by the time anyone tries to name them, and reading them back within the same run still works, which is what makes the omission easy to miss.

## Deploying the Transcription Service

`services/whisper` deploys on a push to `main` that touches it, and by hand with `modal deploy app.py`. **A deploy that only changes `app.py` does not reach a container that is already up.** The FastAPI app is built once when a container starts, so a route added there is live only after the container turns over: the deploy reports success either way, and the endpoint goes on serving what it was serving. A change inside `modal_whisper/` does not have this problem, because it rebuilds the image and no old container survives it.

The container idles down after two minutes, so waiting is enough — as long as nothing keeps calling it, and polling to see whether the change landed is what keeps it alive. `modal app stop` forces it. Checking whether a route exists takes no credentials: every route answers 401 without a token, and 404 is the route not being there at all.

## Config Files

All stored in `~/.config/plaud/` with 0600 permissions:
- `token.json` — Plaud auth token and device ID
- `auth.json` — the Google sign-in for the transcription service (0600; a refresh token is worth the account)
- `update-state.json` — Version check cache
- `cache/transcripts/` — Local transcript cache

