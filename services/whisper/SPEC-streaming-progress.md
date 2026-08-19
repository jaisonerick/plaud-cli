# Streaming Progress Protocol

## Problem

The `transcribe` method runs a multi-stage pipeline (context extraction → transcription → alignment → diarization → polishing → compaction) that takes 1–3 minutes. The client currently blocks on a single `.Remote()` call with no visibility into what's happening.

The client should not hardcode knowledge of server stages. The server owns the pipeline definition — it declares what stages exist, their display labels, and their order. The client is a dumb renderer: it draws what the server tells it.

## Architecture

The progress display shows a unified list of stages. Some stages are **client-side** (the client measures progress directly), some are **server-side** (the server reports progress via the streaming protocol). The client stitches them into one seamless view.

```
✓ Downloading audio            2.4 MB
✓ Waiting for server
✓ Uploading audio              2.4 MB
✓ Extracting context           5 hotwords
⠋ Transcribing audio           87 segments  12s
  Aligning timestamps
  Diarizing speakers
  Polishing transcript
  Compacting segments
  Saving transcript
```

### Client-side stages

These stages run locally — the client controls their lifecycle and can measure progress directly. They are **not part of the server protocol**; the client creates and manages them independently.

#### Download audio

The client downloads the recording from the Plaud API. Progress is measurable via HTTP `Content-Length` header and bytes received.

**How to monitor:** Wrap `resp.Body` in a counting `io.Reader`. On each read, emit a progress event with `current` bytes / `total` bytes (from `Content-Length`). The detail text shows percentage and size (e.g., `"45%  1.1 MB"`).

```go
// In api/client.go — FetchFile gains a progress callback
func (c *Client) FetchFile(ctx context.Context, fileURL string, onProgress func(received, total int64)) ([]byte, error) {
    resp, _ := c.HTTP.Do(req)
    total := resp.ContentLength  // -1 if unknown
    reader := &progressReader{r: resp.Body, total: total, onProgress: onProgress}
    return io.ReadAll(reader)
}
```

The command layer translates callbacks to tracker events:

```go
tracker.Update(Event{Stage: "download", Status: "started"})
audioData, _ := client.FetchFile(ctx, url, func(received, total int64) {
    pct := received * 100 / total
    tracker.Update(Event{
        Stage:  "download",
        Status: "progress",
        Detail: fmt.Sprintf("%d%%  %.1f MB", pct, float64(received)/1e6),
    })
})
tracker.Update(Event{Stage: "download", Status: "done", Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})
```

#### Connect + Upload audio

The client sends the audio bytes to the server as a multipart POST. Two stages cover the full lifecycle:

1. **Connect** — starts when the HTTP request is initiated. Ends when the first byte of the request body is read by the server (detected via a counting `io.Reader`). On cold starts, this stage shows "Waiting for server" with an elapsed timer for 30–60s. On warm containers, it flashes through instantly.

2. **Upload** — starts when the first body byte is read. Progress is tracked via the counting reader (bytes sent / total). Ends when the server's first SSE event (`init`) arrives.

```go
tracker.Update(Event{Stage: "connect", Status: "started"})
// ... TranscribeStream detects first body byte read ...
tracker.Update(Event{Stage: "connect", Status: "done"})
tracker.Update(Event{Stage: "upload", Status: "started",
    Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})
// ... body bytes flow, progress updates ...
// ... "init" event arrives ...
tracker.Update(Event{Stage: "upload", Status: "done",
    Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})
```

#### Save transcript

The client writes the result to disk. This is effectively instantaneous.

```go
tracker.Update(Event{Stage: "save", Status: "started"})
os.WriteFile(dest, content, 0644)
tracker.Update(Event{Stage: "save", Status: "done"})
```

### Server-side stages

These stages run on the Modal server. The client learns about them dynamically through the streaming protocol — it never hardcodes stage IDs or labels.

---

## Transport: SSE over FastAPI

The server exposes a single `POST /transcribe` FastAPI endpoint on Modal. The endpoint accepts audio data and transcription options as a multipart form POST. The `stream` query parameter controls the response format:

- `?stream=true` — returns `text/event-stream` (SSE). Each event is a JSON object in the `data:` field.
- No `stream` param or `stream=false` — returns the final result as a single `application/json` response (backward-compatible).

### Authentication

Uses Modal's **proxy auth** (`requires_proxy_auth=True`). The client sends the Modal token as:

```
Modal-Key: <token_id>
Modal-Secret: <token_secret>
```

This is validated at Modal's proxy layer before reaching the container — no custom auth code needed.

### Request format

```
POST /transcribe?stream=true
Content-Type: multipart/form-data
Modal-Key: <token_id>
Modal-Secret: <token_secret>

Parts:
  audio: <binary audio data>
  options: <JSON string>
```

The `options` JSON:

```json
{
  "diarize": true,
  "polish": true,
  "compact": true,
  "compact_gap": 2000,
  "language": "pt",
  "context_doc": "Meeting agenda: ...",
  "speaker_recognition": true,
  "speaker_threshold": 0.35
}
```

### SSE response format

When `stream=true`, the response is `Content-Type: text/event-stream`. Each event follows the SSE spec:

```
data: {"type": "init", "stages": [...]}

data: {"type": "update", "stage": "transcribe", "status": "started"}

data: {"type": "result", "audio_id": "...", "segments": [...], "speakers": {...}}

```

Each `data:` line contains a complete JSON object. Events are separated by blank lines per the SSE spec.

### Non-streaming response format

When `stream=false` (or omitted), the endpoint runs the full pipeline and returns:

```
HTTP/1.1 200 OK
Content-Type: application/json

{"audio_id": "...", "segments": [...], "speakers": {...}}
```

---

## Server protocol

### Event types

#### `init` — Pipeline declaration (first event, exactly once)

Declares the stages that will run in this request. The client uses this to set up its progress display. Stages appear in execution order. Only stages that will actually execute are listed — the server omits disabled stages (e.g., no `diarize` stage if diarization is off).

```json
{
  "type": "init",
  "stages": [
    {"id": "context",            "label": "Extracting context"},
    {"id": "transcribe",         "label": "Transcribing audio"},
    {"id": "align",              "label": "Aligning timestamps"},
    {"id": "diarize",            "label": "Diarizing speakers"},
    {"id": "speaker_assign",     "label": "Assigning speakers"},
    {"id": "segment_convert",    "label": "Converting segments"},
    {"id": "speaker_recognition","label": "Recognizing speakers"},
    {"id": "polish",             "label": "Polishing transcript"},
    {"id": "compact",            "label": "Compacting segments"}
  ]
}
```

#### `update` — Stage status change

Reports that a stage has started, made progress, or finished.

```json
{
  "type": "update",
  "stage": "transcribe",
  "status": "started",
  "detail": null,
  "progress": null
}
```

Fields:

| Field | Type | Description |
|-------|------|-------------|
| `stage` | `string` | Stage `id` from the `init` event |
| `status` | `string` | One of: `started`, `progress`, `done` |
| `detail` | `string \| null` | Right-side text to display (e.g., `"142 segments"`, `"3 speakers"`) |
| `progress` | `object \| null` | Only for `status: "progress"`. See below. |

The `progress` object (when present):

```json
{
  "current": 3,
  "total": 8
}
```

The client renders this as `"3/8"` (or any format it chooses). The `detail` field, when set alongside `progress`, takes precedence for display — the client may show both or just `detail`.

#### `result` — Final output (last event, exactly once)

The complete transcription result. Same schema as the current return value.

```json
{
  "type": "result",
  "audio_id": "uuid",
  "segments": [{"start_time": 0, "end_time": 5200, "content": "...", "speaker": "Alice"}],
  "speakers": {"SPEAKER_00": "Alice", "SPEAKER_01": "Bob"}
}
```

#### `error` — Pipeline failure (terminal)

If the pipeline fails, this is the last event. The client should stop rendering and display the error.

```json
{
  "type": "error",
  "stage": "diarize",
  "message": "HuggingFace token not configured"
}
```

`stage` is optional — it identifies which stage failed (if known).

### Status lifecycle

Each stage follows this lifecycle:

```
started → [progress]* → done
```

- `started` — stage begins. The client starts the spinner and elapsed timer.
- `progress` — intermediate update. The `detail` text changes and/or `progress.current` increments. Can be emitted zero or more times.
- `done` — stage finished. The client stops the spinner and shows the final `detail` text (e.g., `"3 speakers"`, `"47 paragraphs"`).

A stage that appears in `init` but never receives a `started` event should remain in "pending" state in the display.

### Detail text conventions

The `detail` field is a short, human-readable string shown to the right of the stage label. It is **set by the server** — the client renders it as-is.

Guidelines for server implementors:

| Stage | During progress | On done |
|-------|----------------|---------|
| Context extraction | — | `"5 hotwords"` |
| Transcription | `"87 segments"` (count as Whisper produces them) | `"142 segments"` |
| Alignment | — | — |
| Diarization | — | `"3 speakers"` |
| Speaker assignment | — | — |
| Segment conversion | — | `"142 segments"` |
| Speaker recognition | — | `"2 matched"` |
| Polishing | `"3/8 chunks"` (via `progress` field) | `"8 chunks"` |
| Compaction | — | `"47 paragraphs"` |

These are examples, not a contract. The server can change labels and details freely — the client just renders whatever it receives.

---

## Server implementation

### App structure in `app.py`

The `WhisperTranscriber` class is preserved — `@modal.enter()` loads the Whisper model once at container startup, and the loaded model is shared across requests. The class exposes a single ASGI app via `@modal.asgi_app()` that mounts a full FastAPI router with all routes.

```python
from fastapi import FastAPI, Request, UploadFile, File, Form, HTTPException
from fastapi.responses import StreamingResponse, JSONResponse
import json
import modal

app = modal.App("modal-whisper")

# ... image, volume definitions ...

@app.cls(
    gpu="A10G",
    image=image,
    secrets=[
        modal.Secret.from_name("huggingface-secret"),
        modal.Secret.from_name("openrouter-secret"),
    ],
    volumes={"/cache": model_cache},
    timeout=600,
    scaledown_window=120,
)
class WhisperTranscriber:
    @modal.enter()
    def setup(self):
        """Load expensive resources once — never mutated after this."""
        from modal_whisper.transcribe import WhisperModel
        from modal_whisper.llm import LLMClient
        from modal_whisper.prompts import set_prompts_dir

        os.environ["HF_HOME"] = "/cache/huggingface"
        set_prompts_dir("/prompts")

        # Long-lived: Whisper model weights on GPU
        self.whisper_model = WhisperModel(
            device="cuda", compute_type="float16", model_name="large-v3",
        )
        self.whisper_model.load()

        # Long-lived: LLM client config (stateless, no mutable state)
        self.llm = LLMClient(
            model="openrouter/anthropic/claude-sonnet-4",
            api_key=os.environ["OPENROUTER_API_KEY"],
        )

    @modal.asgi_app(label="modal-whisper", requires_proxy_auth=True)
    def web(self):
        web_app = FastAPI()

        @web_app.post("/transcribe")
        async def transcribe(
            audio: UploadFile = File(...),
            options: str = Form("{}"),
            stream: bool = False,
        ):
            from modal_whisper.builder import TranscribeOptions

            audio_data = await audio.read()
            opts_json = json.loads(options)

            # Build per-request options — no builder mutation, concurrency-safe
            opts = TranscribeOptions(
                language=opts_json.get("language", ""),
                context_doc=opts_json.get("context_doc", ""),
                diarize=opts_json.get("diarize", True),
                speaker_recognition=opts_json.get("speaker_recognition", False),
                speaker_threshold=opts_json.get("speaker_threshold", 0.35),
                polish=opts_json.get("polish", True),
                compact=opts_json.get("compact", True) and opts_json.get("diarize", True),
                compact_gap=opts_json.get("compact_gap", 2000),
            )

            if stream:
                def generate():
                    try:
                        pipeline = TranscriptionPipeline(
                            self.whisper_model, self.llm,
                        )
                        for event in pipeline.transcribe_stream(audio_data, opts):
                            yield f"data: {json.dumps(event)}\n\n"
                    finally:
                        model_cache.commit()
                return StreamingResponse(generate(), media_type="text/event-stream")
            else:
                pipeline = TranscriptionPipeline(
                    self.whisper_model, self.llm,
                )
                result = pipeline.transcribe(audio_data, opts.language)
                model_cache.commit()
                return JSONResponse(result)

        @web_app.put("/speakers/{audio_id}/{speaker_id}")
        async def set_speaker_name(audio_id: str, speaker_id: str, name: str = Form(...)):
            from modal_whisper.speaker_store import SpeakerStore

            store = SpeakerStore()
            embedding = store.get_audio_speaker_info(audio_id, speaker_id)
            if embedding is None:
                store.close()
                raise HTTPException(
                    status_code=404,
                    detail=f"No embedding found for {audio_id}/{speaker_id}",
                )

            store.set_known_speaker(name, embedding)
            store.close()
            model_cache.commit()
            return {
                "success": True,
                "name": name,
                "audio_id": audio_id,
                "speaker_id": speaker_id,
            }

        @web_app.get("/speakers")
        async def list_known_speakers():
            from modal_whisper.speaker_store import SpeakerStore

            store = SpeakerStore()
            names = store.get_known_speaker_names()
            store.close()
            return names

        return web_app
```

Key points:
- **`@modal.asgi_app()`** mounts a full FastAPI app, giving us proper RESTful routes (`/transcribe`, `/speakers/{audio_id}/{speaker_id}`, `/speakers`) instead of flat function-name paths.
- **`@modal.enter()`** loads the model once — `self.model` is available to all routes via closure.
- **`try/finally`** in the streaming generator ensures `model_cache.commit()` runs after the last event is yielded, even if the client disconnects mid-stream.
- **All routes share the GPU container** — speaker endpoints don't need GPU but share the warm container, avoiding cold start latency for quick operations. If cost becomes an issue, they can be split to a separate non-GPU class later.
- **`requires_proxy_auth=True`** on the ASGI app applies to all routes.

**Ownership model — long-lived vs per-request state:**

The `@modal.enter()` method loads expensive, immutable resources that persist for the container's lifetime. Each request creates short-lived objects that reference those resources without mutating them.

| Layer | Lifetime | What lives here |
|-------|----------|----------------|
| **Container** (`@modal.enter()`) | Minutes–hours | Whisper model weights (base, no hotwords), LLMClient (stateless config), device/compute config |
| **Request** (`transcribe_stream()`) | Seconds–minutes | `TranscribeOptions`, loaded audio array, hotword-augmented model (if needed), Diarizer, Polisher, Compactor, SegmentConverter, SpeakerStore |

This requires restructuring `Transcriber`:

```python
class WhisperModel:
    """Long-lived: loaded once at container startup, never mutated."""

    def __init__(self, device: str, compute_type: str, model_name: str):
        self.device = device
        self.compute_type = compute_type
        self.model_name = model_name
        self.model = None  # set by load()

    def load(self):
        """Load base Whisper model (no hotwords). Called once."""
        self.model = whisperx.load_model(
            self.model_name, self.device, compute_type=self.compute_type,
        )

    def with_hotwords(self, hotwords: str) -> "whisperx model":
        """Return a new model instance with hotwords. Does NOT mutate self.
        Expensive (~2-3s) — reloads model weights with ASR options."""
        if not hotwords:
            return self.model
        return whisperx.load_model(
            self.model_name, self.device, compute_type=self.compute_type,
            asr_options={"hotwords": hotwords},
        )


class TranscribeSession:
    """Per-request: created in transcribe_stream(), garbage collected after."""

    def __init__(self, whisper_model: WhisperModel, hotwords: str = ""):
        self.device = whisper_model.device
        self._model = whisper_model.with_hotwords(hotwords)
        self.audio = None

    def load_audio(self, audio_data: bytes):
        """Write audio bytes to temp file and load via whisperx."""
        # ... same as current Transcriber.load_audio() ...

    def run(self, language: str = "") -> dict:
        """Transcribe and align. Returns whisperx result dict."""
        # ... same as current Transcriber.run(), uses self._model and self.audio ...
```

The builder's `@modal.enter()` creates a `WhisperModel`. Each request creates a `TranscribeSession` that gets a model reference (or a hotword-augmented copy). When the request ends, the session and its audio array are garbage collected. The base `WhisperModel` is never mutated.

**`TranscribeOptions` rationale:** The web endpoint passes all options directly to `transcribe_stream()` via `TranscribeOptions` — no builder mutation, no `with_*()` calls. The builder pattern (`with_diarize()`, `with_polish()`, etc.) remains available for non-web callers but is not used by the ASGI endpoint.

### Pipeline changes (`modal_whisper/builder.py`)

The `WhisperTranscriptionBuilder` is split into:

- **`WhisperModel`** (in `transcribe.py`) — long-lived, loaded once, holds model weights. See ownership model above.
- **`TranscribeSession`** (in `transcribe.py`) — per-request, holds audio + hotword-augmented model.
- **`TranscriptionPipeline`** (replaces builder) — per-request, orchestrates all stages. References long-lived `WhisperModel` and `LLMClient` but never mutates them.
- **`TranscribeOptions`** — per-request value object with all pipeline config.

`TranscriptionPipeline.transcribe_stream()` takes all options as explicit arguments — no mutation of shared state. The existing `transcribe()` delegates to it. The `with_*()` builder methods are removed — the web endpoint passes `TranscribeOptions` directly.

```python
@dataclass
class TranscribeOptions:
    """Per-request options — no shared state."""
    language: str = ""
    context_doc: str = ""
    diarize: bool = False
    speaker_recognition: bool = False
    speaker_threshold: float = 0.35
    polish: bool = False
    compact: bool = False
    compact_gap: int = 2000


class TranscriptionPipeline:
    """Per-request pipeline. References long-lived resources, never mutates them."""

    def __init__(self, whisper_model: WhisperModel, llm: LLMClient,
                 speaker_db_path: str = "/cache/speakers.db"):
        self._whisper_model = whisper_model  # long-lived, read-only
        self._llm = llm                      # long-lived, stateless
        self._speaker_db_path = speaker_db_path

    def transcribe_stream(self, audio_data: bytes, opts: TranscribeOptions) -> Iterator[dict]:
        """Execute pipeline as a generator, yielding progress events.

        All options are in `opts` — no reads from self._do_* state.
        Safe to call concurrently from multiple ASGI requests.
        """
        audio_id = str(uuid.uuid4())

        # 1. Declare pipeline stages
        stages = []
        if opts.context_doc:
            stages.append({"id": "context", "label": "Extracting context"})
        stages.append({"id": "transcribe", "label": "Transcribing audio"})
        stages.append({"id": "align", "label": "Aligning timestamps"})
        if opts.diarize:
            stages.append({"id": "diarize", "label": "Diarizing speakers"})
            stages.append({"id": "speaker_assign", "label": "Assigning speakers"})
        stages.append({"id": "segment_convert", "label": "Converting segments"})
        if opts.speaker_recognition and opts.diarize:
            stages.append({"id": "speaker_recognition", "label": "Recognizing speakers"})
        if opts.polish:
            stages.append({"id": "polish", "label": "Polishing transcript"})
        if opts.compact and opts.diarize:
            stages.append({"id": "compact", "label": "Compacting segments"})

        yield {"type": "init", "stages": stages}

        # 2. Context extraction (lazy — runs inside the stream)
        hotwords = ""
        context_summary = ""
        if opts.context_doc:
            yield {"type": "update", "stage": "context", "status": "started"}
            ctx = ContextExtractor(self._llm, opts.context_doc).run()
            context_summary = ctx.context_summary
            hotwords = ctx.hotwords
            n_hotwords = len([h for h in hotwords.split(",") if h.strip()])
            yield {"type": "update", "stage": "context", "status": "done",
                   "detail": f"{n_hotwords} hotwords"}

        # 3. Transcription — per-request session, never mutates self._whisper_model
        yield {"type": "update", "stage": "transcribe", "status": "started"}
        session = TranscribeSession(self._whisper_model, hotwords=hotwords)
        session.load_audio(audio_data)
        result = session.run(opts.language)
        seg_count = len(result.get("segments", []))
        yield {"type": "update", "stage": "transcribe", "status": "done",
               "detail": f"{seg_count} segments"}

        # 4. Alignment (uses session.audio and session.device)
        yield {"type": "update", "stage": "align", "status": "started"}
        result = session.align(result)
        yield {"type": "update", "stage": "align", "status": "done"}

        # 5–10. Diarization, speaker recognition, polishing, compaction
        # ... (same pattern — all reads from `opts`, not self._do_*) ...

        # Final result
        yield {"type": "result", "audio_id": audio_id,
               "segments": segments, "speakers": speaker_map}

    def transcribe(self, audio_data: bytes, opts: TranscribeOptions) -> dict:
        """Execute pipeline, return final result dict.
        Delegates to transcribe_stream() — no duplicate logic.
        """
        result = None
        for event in self.transcribe_stream(audio_data, opts):
            if event["type"] == "result":
                result = event
        return {k: v for k, v in result.items() if k != "type"}
```

### Polisher changes (`modal_whisper/polish.py`)

Add `run_iter()` that yields polished chunks as each completes:

```python
def run_iter(self, segments: list[dict]) -> Iterator[list[dict]]:
    """Yield polished chunks as each completes, in order."""
    chunks = self._chunk(segments)
    prompt = load_prompt("polish").format(context_summary=self.context_summary)
    messages_list = [
        [{"role": "system", "content": prompt},
         {"role": "user", "content": self._format(chunk)}]
        for chunk in chunks
    ]

    for i, response in enumerate(self.llm.call_batch_iter(messages_list)):
        yield self._parse(response.strip(), chunks[i])
```

### LLM changes (`modal_whisper/llm.py`)

Add `call_batch_iter()` that yields results in submission order as futures complete:

```python
def call_batch_iter(self, messages_list):
    """Yield responses as they complete, in submission order."""
    results = [None] * len(messages_list)
    completed = [False] * len(messages_list)
    next_idx = 0

    with ThreadPoolExecutor(max_workers=self.max_workers) as pool:
        futures = {
            pool.submit(self.call, msgs): i
            for i, msgs in enumerate(messages_list)
        }
        for future in as_completed(futures):
            idx = futures[future]
            results[idx] = future.result()
            completed[idx] = True

            # Yield in order
            while next_idx < len(results) and completed[next_idx]:
                yield results[next_idx]
                next_idx += 1
```

### API routes summary

All routes are served from the same `@modal.asgi_app()` on the `WhisperTranscriber` class:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/transcribe` | Transcribe audio. `?stream=true` for SSE, otherwise JSON. |
| `PUT` | `/speakers/{audio_id}/{speaker_id}` | Register a speaker name from a transcription's embedding. |
| `GET` | `/speakers` | List all known speaker names. |

All routes share the GPU container and proxy auth. Speaker endpoints don't need GPU but benefit from the warm container — no separate cold start. If cost becomes an issue, they can be split to a non-GPU class later.

---

## Client implementation

### New package: `internal/modal/client.go` — HTTP transport layer

A new `ModalHTTPClient` handles HTTP communication with the Modal FastAPI endpoint. It encapsulates auth, multipart encoding, and SSE parsing. The transcribe command consumes it as an event iterator.

```go
package modal

// ModalHTTPClient handles HTTP communication with Modal FastAPI endpoints.
type ModalHTTPClient struct {
    endpointURL string  // e.g. "https://jaisonerick--modal-whisper.modal.run"
    tokenID     string
    tokenSecret string
    httpClient  *http.Client
}

// TranscribeStream sends audio + options and returns an SSE event channel.
// The caller iterates events exactly like a generator.
func (c *ModalHTTPClient) TranscribeStream(ctx context.Context, audioData []byte, opts TranscribeOpts) (<-chan SSEEvent, <-chan error)

// SSEEvent represents a parsed server event.
type SSEEvent struct {
    Type     string          // "init", "update", "result", "error"
    Raw      json.RawMessage // full JSON for flexible parsing
    // Parsed fields for convenience:
    Stages   []StageDef      // populated for "init"
    Stage    string           // populated for "update"
    Status   string           // populated for "update"
    Detail   *string          // populated for "update" (nullable)
    Progress *Progress        // populated for "update" with progress
    Result   *TranscribeResult // populated for "result"
    Error    *string          // populated for "error"
}
```

Usage from the command layer — looks like iterating a generator:

```go
events, errs := httpClient.TranscribeStream(ctx, audioData, opts)

for evt := range events {
    switch evt.Type {
    case "init":
        tracker.AddStages(evt.Stages)
    case "update":
        tracker.Update(progress.Event{
            Stage:   evt.Stage,
            Status:  evt.Status,
            Detail:  derefOr(evt.Detail, ""),
            Current: evt.Progress.Current,
            Total:   evt.Progress.Total,
        })
    case "result":
        result = evt.Result
    case "error":
        return fmt.Errorf("server error at %s: %s", evt.Stage, *evt.Error)
    }
}
if err := <-errs; err != nil {
    return fmt.Errorf("stream error: %w", err)
}
```

### SSE parsing

We don't need reconnection, event IDs, retry, or named event types — just `data:` lines. Manual parsing with `bufio.Scanner` + `strings.TrimPrefix` is sufficient and avoids a dependency. The parser is ~20 lines:

```go
scanner := bufio.NewScanner(resp.Body)
// The "result" event contains the entire transcript — can exceed the default
// 64KB scanner buffer for long recordings. 4MB is generous for ~1hr audio.
scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") {
        continue // skip blank lines and comments
    }
    payload := strings.TrimPrefix(line, "data: ")
    // parse JSON from payload...
}
```

### Tracker changes (`internal/progress/progress.go`)

The `Tracker` no longer needs hardcoded `Stage` constants or `stageLabels`. Instead:

1. Remove all server-side `Stage` constants (`StageContextExtraction`, `StageTranscribe`, etc.) and `stageLabels` map
2. Keep only client-side constants (`StageDownload`, `StageSave`) for the stages the client owns
3. Stage IDs become opaque strings — the tracker doesn't interpret them
4. Add `AddStages(stages []StageDef)` to dynamically insert server-declared stages after construction

```go
type StageDef struct {
    ID    string `json:"id"`
    Label string `json:"label"`
}

// NewTracker creates a tracker with initial client-side stages.
func NewTracker(w io.Writer, clientStages []StageDef) *Tracker

// AddStages appends server-declared stages to the display.
// Called once when the "init" event arrives.
func (t *Tracker) AddStages(stages []StageDef)
```

### Removing the Modal SDK dependency

The entire Modal Go SDK (`github.com/modal-labs/modal-client/go`) is removed from the client. All communication goes through HTTP:

- **Transcribe** → `POST /transcribe?stream=true` via `ModalHTTPClient.TranscribeStream()`
- **Set speaker name** → `PUT /speakers/{audio_id}/{speaker_id}` via `ModalHTTPClient.SetSpeakerName()`
- **List speakers** → `GET /speakers` via `ModalHTTPClient.ListKnownSpeakers()`

The `ModalHTTPClient` handles auth headers (`Modal-Key`, `Modal-Secret`) and JSON encoding/decoding for all three. The existing `internal/modal/transcribe.go` with its `convertToJSON`, `getInstance`, and Modal SDK plumbing is deleted.

### Client event loop (`cmd/transcribe.go`)

```go
// Phase 1: Client-side download
tracker := progress.NewTracker(os.Stderr, []progress.StageDef{
    {ID: "download", Label: "Downloading audio"},
    {ID: "connect",  Label: "Waiting for server"},
    {ID: "upload",   Label: "Uploading audio"},
})

tracker.Update(Event{Stage: "download", Status: "started"})
audioData, _ := client.FetchFile(ctx, url, func(received, total int64) {
    pct := received * 100 / total
    tracker.Update(Event{Stage: "download", Status: "progress",
        Detail: fmt.Sprintf("%d%%  %.1f MB", pct, float64(received)/1e6)})
})
tracker.Update(Event{Stage: "download", Status: "done",
    Detail: fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)})

// Phase 2: Connect + upload + server streaming
tracker.Update(Event{Stage: "connect", Status: "started"})

sizeMB := fmt.Sprintf("%.1f MB", float64(len(audioData))/1e6)
events, errs := httpClient.TranscribeStream(ctx, audioData, opts, modal.StreamCallbacks{
    OnUploadStart: func() {
        // First body byte read — server is warm, upload begins
        tracker.Update(Event{Stage: "connect", Status: "done"})
        tracker.Update(Event{Stage: "upload", Status: "started", Detail: sizeMB})
    },
    OnUploadProgress: func(sent, total int64) {
        pct := sent * 100 / total
        tracker.Update(Event{Stage: "upload", Status: "progress",
            Detail: fmt.Sprintf("%d%%  %s", pct, sizeMB)})
    },
})

var result *modal.TranscribeResult
for evt := range events {
    switch evt.Type {
    case "init":
        // First server event — upload complete, server processing
        tracker.Update(Event{Stage: "upload", Status: "done", Detail: sizeMB})
        // Insert server stages, then save
        tracker.AddStages(evt.Stages)
        tracker.AddStages([]progress.StageDef{{ID: "save", Label: "Saving transcript"}})

    case "update":
        tracker.Update(progress.Event{
            Stage:   evt.Stage,
            Status:  evt.Status,
            Detail:  derefOr(evt.Detail, ""),
            Current: progressCurrent(evt.Progress),
            Total:   progressTotal(evt.Progress),
        })

    case "result":
        result = evt.Result

    case "error":
        tracker.Wait()
        return fmt.Errorf("transcription failed: %s", *evt.Error)
    }
}
if err := <-errs; err != nil {
    return fmt.Errorf("stream error: %w", err)
}

// Phase 3: Client-side save
tracker.Update(Event{Stage: "save", Status: "started"})
os.WriteFile(dest, content, 0644)
tracker.Update(Event{Stage: "save", Status: "done"})

tracker.Wait()
```

---

## Wire format

SSE (Server-Sent Events) per the [W3C spec](https://html.spec.whatwg.org/multipage/server-sent-events.html):

```
data: {"type": "init", "stages": [...]}\n
\n
data: {"type": "update", "stage": "transcribe", "status": "started"}\n
\n
data: {"type": "result", ...}\n
\n
```

Each event is a single `data:` line containing a complete JSON object, followed by a blank line. The client should handle unknown event types gracefully (ignore them) for forward compatibility.

## Cold start and upload latency

Between the client sending the HTTP request and the first `init` event arriving, two things happen sequentially:

1. **Container cold start** — If no warm container exists, Modal's proxy holds the connection while it provisions a GPU instance and loads the Whisper model (30–60s). During this time, the proxy has not started reading the request body — the client's bytes are queued.
2. **Audio upload** — Once the container is warm, the proxy forwards the request body. Bytes flow to the server, and once fully received, the server starts processing and emits the first SSE event.

### Detecting cold start vs upload

The client wraps the multipart request body in a counting `io.Reader`. By observing **whether bytes are being read from the body**, the client can distinguish the two phases:

- **No bytes read yet** → Modal proxy is holding the connection (cold start). Show "Waiting for server..."
- **Bytes are being read** → Upload is in progress. Show percentage and size.
- **`init` event arrives** → Server received data and started processing. Upload complete.

The client uses two stages for this: a `connect` stage and an `upload` stage.

```
⠋ Waiting for server                    12s    ← no body bytes read
✓ Waiting for server                            ← first byte read, connect done
⠋ Uploading audio        45%  1.1 MB     3s    ← body bytes flowing
✓ Uploading audio              2.4 MB           ← init event arrived
⠋ Extracting context                     1s
  Transcribing audio
  ...
```

Implementation in `TranscribeStream`:

```go
// Create a counting reader that wraps the multipart body
body := &countingReader{
    r:       multipartBody,
    total:   bodySize,
    onFirst: func() {
        // First byte read — cold start is over, upload begins
        onUploadStart()
    },
    onProgress: func(n, total int64) {
        onUploadProgress(n, total)
    },
}

req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, body)
```

When the container is **already warm** (within the 120s scaledown window), the first body byte is read almost immediately, so the `connect` stage flashes through instantly. The user only sees it on cold starts.

## Endpoint URL configuration

The client needs the Modal ASGI app base URL. With `@modal.asgi_app(label="modal-whisper")`, Modal assigns the URL:

```
https://<workspace>--modal-whisper.modal.run
```

For example: `https://jaisonerick--modal-whisper.modal.run`

All routes are under this base: `/transcribe`, `/speakers/{audio_id}/{speaker_id}`, `/speakers`.

This URL is stored in the client's config file (`~/.config/plaud/token.json`) alongside Modal credentials. It can be:

- Auto-discovered after `plaud modal-auth` by querying the Modal API for the deployed app's web endpoints
- Manually set via `plaud modal-auth --endpoint <url>`

The `ModalHTTPClient` reads this from config at construction time.

## Error handling

- **Connection drop during streaming**: The client should treat this as a fatal error. No reconnection — the pipeline is stateful and cannot resume. Display whatever stages completed and report the error.
- **Pipeline stage failure**: The server emits an `error` event and closes the stream. Non-fatal failures (e.g., diarization fails but pipeline continues) are reported as a `done` status with a detail like `"failed"`, not as `error` events.
- **HTTP errors (401, 500)**: Handled before SSE parsing begins. The client checks the response status code before attempting to read the event stream.

## Migration

1. **Server**: Replace all `@modal.method()` with a single `@modal.asgi_app()` on `WhisperTranscriber` that mounts a FastAPI app with all routes. `@modal.enter()` loads `WhisperModel` + `LLMClient` (long-lived, immutable). Split `WhisperTranscriptionBuilder` into `WhisperModel` (in `transcribe.py`), `TranscribeSession` (per-request, in `transcribe.py`), and `TranscriptionPipeline` (per-request orchestrator, replaces builder). Add `TranscribeOptions` dataclass. `transcribe` becomes `POST /transcribe` with SSE streaming. `set_speaker_name` becomes `PUT /speakers/{audio_id}/{speaker_id}`. `list_known_speakers` becomes `GET /speakers`. Add `run_iter()` to Polisher. Add `call_batch_iter()` to LLMClient.
2. **Client**: Add `ModalHTTPClient` with SSE support for all endpoints. Remove `github.com/modal-labs/modal-client/go` dependency entirely. Update `cmd/transcribe.go` to use streaming event loop with download/upload/save client stages. Make `Tracker` dynamic (remove hardcoded server stages, add `AddStages`). Add progress callback to `FetchFile`. Store endpoint URL in config.
3. **Deploy**: Server first (supports both stream and non-stream via `?stream=true`). Then client update.
