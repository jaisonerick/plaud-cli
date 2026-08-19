# modal-whisper Improvement Spec

## Current State

Whisper+WhisperX pipeline on Modal producing sentence-level segments with speaker diarization and vocabulary hints. Compared against Plaud native on a 1h11m pt-BR meeting (4 speakers):

- **Word accuracy**: Whisper better on technical terms (ociosidade, abastecimento, ondas) and brand names (Gran Cru, Evino, Solfácil) via hotwords
- **Content volume**: Plaud captures slightly more (11,630 vs 11,414 words), especially in the last 10 minutes where Whisper drops off
- **Diarization**: Working (99.7% segments labeled, 4 speakers detected), 93.6% agreement with Plaud on speaker attribution. In a manual review of 5 disagreement samples, Whisper was correct in all 5 cases.
- **Segment granularity**: 619 sentence-level segments vs Plaud's 208 paragraph-level
- **Overlapping speech**: Neither system handles simultaneous speakers — both serialize into non-overlapping, single-speaker segments. When two people talk at once, the dominant voice is transcribed and the other is misattributed or dropped. Whisper shows 9.0% uncovered time vs Plaud's 5.5%, suggesting Whisper loses more crosstalk content.

## Planned Improvements

### 1. Pre-processing: AI-guided audio chunking

**Problem**: Whisper's attention degrades on long audio (>30 min), causing content loss in later sections. The last 10 min of a 71-min recording lost ~30% content vs Plaud.

**Approach**:
- Split audio into overlapping chunks before transcription (e.g. 10-min chunks with 30s overlap)
- Use VAD (voice activity detection) to find natural silence points for splitting, avoiding mid-sentence cuts
- Each chunk is transcribed independently, preserving full model attention
- Overlap regions are used for alignment during the merge step

**Parameters to expose**:
- `chunk_duration`: Target chunk length in seconds (default: 600)
- `chunk_overlap`: Overlap between chunks in seconds (default: 30)

**Implementation**: In `app.py`, before the transcription step. Use whisperx's `load_audio` + silence detection to find split points.

### 2. AI-powered segment merging (post-chunking)

**Problem**: Chunked transcription produces duplicate/overlapping content at chunk boundaries. Naive concatenation creates repeated text and broken sentences.

**Approach**:
- After transcribing all chunks, pass the overlap regions to an LLM (Claude) to intelligently merge:
  - Detect duplicate content in overlap zones
  - Choose the better transcription when chunks disagree
  - Preserve sentence boundaries and speaker continuity
  - Reconcile speaker IDs across chunks (SPEAKER_00 in chunk 1 may be SPEAKER_02 in chunk 2)
- The LLM receives both versions of the overlap region with timestamps and produces a clean merged segment list

**Implementation**: New merge step after transcription, before formatting. Calls Claude API with structured prompt containing the two overlapping segment lists.

### 3. Speaker paragraph compaction

**Problem**: Whisper produces 619 sentence-level segments. Consecutive segments from the same speaker are fragmented, making the transcript hard to read as prose. Plaud's 208 paragraph-level segments are more scannable.

**Approach**:
- After diarization, merge consecutive segments from the same speaker into paragraphs
- Preserve the first segment's `start_time` and last segment's `end_time`
- Use a configurable gap threshold: if two consecutive same-speaker segments are separated by more than N seconds, keep them separate (indicates a pause or interjection)
- Expose as a formatting option, not baked into the segment contract — raw segments stay sentence-level, compaction happens at output time

**Parameters**:
- `compact`: Enable paragraph compaction (default: false)
- `compact_gap`: Max silence gap in ms before starting a new paragraph (default: 2000)

**Implementation**: In `app.py` as a post-processing step after diarization, before returning segments. The CLI passes `compact` and `compact_gap` as parameters to the Modal function. Raw sentence-level segments are still the internal representation — compaction runs server-side before the response.

### 4. AI post-processing for accuracy

**Problem**: Both Whisper and Plaud make word-level errors. Whisper invents "comerçado", Plaud garbles "abastecimento" → "acessimento". Some errors are detectable by context. Additionally, raw transcripts lack proper punctuation, capitalization, and paragraph structure.

**Approach**:

The post-processing pipeline has two phases: context extraction and transcript polishing.

**Phase 1 — Context extraction**: The CLI passes an optional context document (plain text or markdown) describing the meeting — participants, company names, domain terminology, acronyms, etc. On the Modal side, an LLM call parses this document to produce:
- A **hotwords list** fed to Whisper at transcription time (improving recognition of domain terms, brand names, and participant names)
- A **context summary** used as system context for the polishing prompt (domain, key terms, participant roles)

**Phase 2 — Transcript polishing**: After transcription + diarization, polish the transcript in chunks:
- Fix obviously wrong words using context (e.g. "comerçado" → "compactado")
- Apply known name replacements (e.g. Gerson/Jason/Jorge/Jackson → Jaison)
- Improve punctuation, capitalization, and sentence structure
- Do NOT rewrite meaning, remove content, or summarize — only correct and structure

**Chunking strategy**: LLMs degrade on very long inputs, but too many calls adds latency and stitching complexity. The transcript is split into chunks of ~50 segments (~5-8 minutes of audio). Each chunk is processed independently with the same context summary. Chunks are split at speaker transitions to avoid cutting mid-turn. No overlap between chunks — each segment belongs to exactly one chunk, and the LLM returns corrected segments with the same `start_time`/`end_time`, so stitching is simple concatenation by timestamp.

**Parameters**:
- `polish`: Enable AI post-processing (default: false)
- `context_doc`: Plain text or markdown with meeting context — participants, companies, domain terms, agenda, prep notes. Parsed by LLM to generate hotwords and polishing context. The quality of corrections depends on the richness of this document.

**Implementation**: In `app.py`. Context extraction runs first (before Whisper transcription, to feed hotwords). Polishing runs after transcription + diarization, parallelized across chunks via ThreadPoolExecutor. Both call the LLM via OpenRouter API (requires `openrouter-secret` Modal secret with `OPENROUTER_API_KEY`). The CLI passes `polish` and `context_doc` as parameters.

### 5. Speaker attribution verification

**Problem**: ~6% disagreement between Whisper and Plaud on who's speaking. Manual review showed Whisper correct 5/5 on sampled disagreements — Whisper is the stronger diarizer, but neither handles overlapping speech. When multiple people talk simultaneously, both systems assign the segment to one speaker and drop or misattribute the other.

**Approach**:
- When both Plaud and Whisper transcripts are available, cross-reference speaker attribution and default to trusting Whisper when they disagree
- For overlapping speech recovery (future): investigate source separation (e.g. separating voices before transcription) to capture both speakers when they talk simultaneously. This is a harder problem and not addressed by the current pipeline.

**Implementation**: When both Plaud and Whisper transcripts are available, produce a diff highlighting disagreements in speaker attribution. Default to trusting Whisper when they disagree.

## Implementation Order

1. **Speaker paragraph compaction** — Simplest, pure formatting, no API calls. Immediate readability improvement.
2. **AI post-processing** — High impact on accuracy. Requires Claude API call but transcript is already text, so fast and cheap.
3. **Pre-processing: audio chunking** — Fixes the content drop-off on long recordings. Requires changes to Modal function.
4. **AI segment merging** — Needed once chunking is in place. Requires Claude API in the Modal pipeline.
5. **Speaker attribution verification** — Most complex, depends on having both transcript sources. Build incrementally.

## Architecture Notes

- The segment contract (`segment_schema.json`) stays unchanged — all improvements happen at the processing layer
- All processing happens inside Modal — the CLI is a thin client that passes parameters and receives a clean transcript
- Chunking, merging, AI post-processing, and compaction all run server-side to avoid round-trips and keep the CLI simple
- LLM access is via OpenRouter API, configured as a Modal secret (`openrouter-secret` with `OPENROUTER_API_KEY`)
- Diarize, polish, and compact are all enabled by default — the CLI uses negative flags (`no-diarize`, `no-polish`, `no-compact`) to opt out
- Each feature is independently disableable, except compact which requires diarize
