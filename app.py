import gc
import json
import math
import os
import sys
import tempfile

import modal

app = modal.App("modal-whisper")

image = (
    modal.Image.debian_slim(python_version="3.11")
    .apt_install("ffmpeg", "git")
    .pip_install(
        "whisperx @ git+https://github.com/m-bain/whisperX.git",
        "torch>=2.1",
        "torchaudio>=2.1",
        "openai>=1.0",
    )
)

model_cache = modal.Volume.from_name("whisper-model-cache", create_if_missing=True)


# --- LLM helpers (OpenRouter via OpenAI-compatible API) ---

def _llm_call(messages: list[dict], model: str = "anthropic/claude-sonnet-4") -> str:
    from openai import OpenAI

    client = OpenAI(
        base_url="https://openrouter.ai/api/v1",
        api_key=os.environ["OPENROUTER_API_KEY"],
    )
    response = client.chat.completions.create(
        model=model,
        messages=messages,
        temperature=0,
    )
    return response.choices[0].message.content


# --- Phase 1: Context extraction ---

def _extract_context(context_doc: str) -> dict:
    """Parse a context document into hotwords and a structured context summary.

    Returns {"hotwords": "word1,word2,...", "context_summary": "..."}
    """
    messages = [
        {
            "role": "system",
            "content": (
                "You are a pre-processing assistant for an audio transcription pipeline. "
                "Given a context document about a meeting or recording (this could be a prep file, "
                "agenda, notes, or any description), extract two things:\n\n"
                "1. **hotwords**: A comma-separated list of words the speech recognition model should "
                "prioritize. Extract from the document:\n"
                "   - Full names of participants (first and last)\n"
                "   - Company and organization names\n"
                "   - Product and brand names\n"
                "   - Technical terms, acronyms, and domain jargon\n"
                "   - Include common phonetic misspellings if obvious (e.g. for 'Jaison' include 'Jason,Gerson,Jorge')\n"
                "   Max 50 items.\n\n"
                "2. **context_summary**: A structured summary for a post-processing step that corrects "
                "transcription errors. Include:\n"
                "   - **People**: Full names and roles of all participants\n"
                "   - **Companies**: All companies/organizations mentioned and their relationship\n"
                "   - **Products**: Product names, tools, systems mentioned\n"
                "   - **Topic**: What the meeting is about — agenda, goals, key discussion points\n"
                "   Format as a compact paragraph covering all four areas.\n\n"
                "Respond ONLY with a JSON object: {\"hotwords\": \"...\", \"context_summary\": \"...\"}"
            ),
        },
        {"role": "user", "content": context_doc},
    ]
    raw = _llm_call(messages)
    # Strip markdown code fences if present
    text = raw.strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1] if "\n" in text else text[3:]
        if text.endswith("```"):
            text = text[:-3]
        text = text.strip()
    return json.loads(text)


# --- Phase 2: Transcript polishing ---

_CHUNK_TARGET = 50  # segments per chunk


def _chunk_segments(segments: list[dict]) -> list[list[dict]]:
    """Split segments into chunks of ~_CHUNK_TARGET, breaking at speaker transitions."""
    if len(segments) <= _CHUNK_TARGET:
        return [segments]

    chunks = []
    current = []
    for i, seg in enumerate(segments):
        current.append(seg)
        if len(current) >= _CHUNK_TARGET:
            next_speaker = segments[i + 1].get("speaker") if i + 1 < len(segments) else None
            at_speaker_transition = seg.get("speaker") != next_speaker
            if at_speaker_transition or len(current) >= _CHUNK_TARGET + 10:
                chunks.append(current)
                current = []

    if current:
        chunks.append(current)
    return chunks


def _format_chunk_for_llm(chunk: list[dict]) -> str:
    """Format segments as lightweight XML tags for LLM input/output."""
    lines = []
    for seg in chunk:
        lines.append(f"<segment:{seg['start_time']}>")
        lines.append(seg["content"])
        lines.append("</segment>")
    return "\n".join(lines)


def _parse_llm_response(text: str, chunk: list[dict]) -> list[dict]:
    """Parse the segment-tagged LLM response back into the chunk."""
    import re
    # Extract all <segment:timestamp>content</segment> blocks
    pattern = re.compile(r"<segment:(\d+)>\n?(.*?)\n?</segment>", re.DOTALL)
    matches = pattern.findall(text)

    if len(matches) != len(chunk):
        print(f"Warning: LLM returned {len(matches)} segments, expected {len(chunk)}. Using originals.", file=sys.stderr)
        return chunk

    # Build a lookup by start_time for resilience to reordering
    corrections = {int(ts): content.strip() for ts, content in matches}
    for seg in chunk:
        if seg["start_time"] in corrections:
            seg["content"] = corrections[seg["start_time"]]

    return chunk


def _polish_chunk(
    chunk: list[dict],
    context_summary: str,
) -> list[dict]:
    """Polish a single chunk of transcript segments via LLM."""
    segments_text = _format_chunk_for_llm(chunk)

    messages = [
        {
            "role": "system",
            "content": (
                "You are a transcript post-processor. You receive transcript segments "
                "from a speech-to-text system in this format:\n\n"
                "<segment:START_TIME_MS>\ncontent\n</segment>\n\n"
                "Your job:\n"
                "- Fix obviously wrong words (misspellings, garbled words) using the meeting context\n"
                "- Fix proper nouns — names, companies, brands, technical terms\n"
                "- Improve punctuation and capitalization\n"
                "- Do NOT change meaning, remove content, add content, or summarize\n"
                "- Do NOT replace a word with a different word just because it seems more likely from context. "
                "Only fix words that are clearly misspelled or garbled by the speech-to-text system. "
                "If a word is a valid word but you're unsure if it's the right one, leave it as-is.\n"
                "- Do NOT merge or split segments — return the same number of segments\n"
                "- Keep the exact same <segment:TIMESTAMP> tags\n\n"
                f"Meeting context: {context_summary}\n\n"
                "Respond ONLY with the corrected segments in the same format. No explanation."
            ),
        },
        {"role": "user", "content": segments_text},
    ]
    raw = _llm_call(messages)
    return _parse_llm_response(raw.strip(), chunk)


def _polish_segments(
    segments: list[dict],
    context_summary: str,
) -> list[dict]:
    """Polish transcript segments in chunks, processing in parallel."""
    from concurrent.futures import ThreadPoolExecutor, as_completed

    chunks = _chunk_segments(segments)
    print(f"Polishing transcript: {len(segments)} segments in {len(chunks)} chunks (parallel)", file=sys.stderr)

    results = [None] * len(chunks)

    def process_chunk(idx, chunk):
        print(f"  Polishing chunk {idx+1}/{len(chunks)} ({len(chunk)} segments)...", file=sys.stderr)
        return idx, _polish_chunk(chunk, context_summary)

    with ThreadPoolExecutor(max_workers=len(chunks)) as pool:
        futures = [pool.submit(process_chunk, i, c) for i, c in enumerate(chunks)]
        for future in as_completed(futures):
            idx, polished_chunk = future.result()
            results[idx] = polished_chunk

    polished = []
    for chunk in results:
        polished.extend(chunk)
    return polished


# --- Segment compaction ---

def _compact_segments(segments: list[dict], max_gap_ms: int = 2000) -> list[dict]:
    """Merge consecutive segments from the same speaker into paragraphs.

    Segments are merged when they share a speaker and the gap between them
    is <= max_gap_ms. The merged segment keeps the first start_time and
    last end_time, with content joined by a space.
    """
    if not segments:
        return segments

    compacted = [dict(segments[0])]
    for seg in segments[1:]:
        prev = compacted[-1]
        gap = seg["start_time"] - prev["end_time"]
        if seg["speaker"] and seg["speaker"] == prev["speaker"] and gap <= max_gap_ms:
            prev["end_time"] = seg["end_time"]
            prev["content"] += " " + seg["content"]
        else:
            compacted.append(dict(seg))

    return compacted


# --- Main transcriber ---

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
    def load_models(self):
        import whisperx

        self.device = "cuda"
        self.compute_type = "float16"

        os.environ["HF_HOME"] = "/cache/huggingface"

        self.model = whisperx.load_model(
            "large-v3",
            self.device,
            compute_type=self.compute_type,
        )

    @modal.method()
    def transcribe(
        self,
        audio_data: bytes,
        diarize: bool = True,
        language: str = "",
        hotwords: str = "",
        compact: bool = True,
        compact_gap: int = 2000,
        polish: bool = True,
        context_doc: str = "",
    ) -> list[dict]:
        import whisperx

        hf_token = os.environ.get("HF_TOKEN", "")

        # Phase 1: Extract context and hotwords from context document
        context_summary = ""
        if context_doc:
            print("Extracting context from document...", file=sys.stderr)
            extracted = _extract_context(context_doc)
            context_summary = extracted.get("context_summary", "")
            # LLM-generated hotwords are used unless caller provided explicit hotwords
            if not hotwords:
                hotwords = extracted.get("hotwords", "")
            print(f"  Context: {context_summary[:100]}...", file=sys.stderr)
            print(f"  Hotwords: {hotwords[:100]}...", file=sys.stderr)

        # Write audio bytes to a temp file for whisperx
        with tempfile.NamedTemporaryFile(suffix=".mp3", delete=False) as f:
            f.write(audio_data)
            audio_path = f.name

        try:
            # 1. Transcribe (optionally with forced language and hotwords)
            audio = whisperx.load_audio(audio_path)

            # Reload model with hotwords if provided (asr_options must be set at load time)
            model = self.model
            if hotwords:
                model = whisperx.load_model(
                    "large-v3",
                    self.device,
                    compute_type=self.compute_type,
                    asr_options={"hotwords": hotwords},
                )

            transcribe_kwargs = {"batch_size": 16}
            if language:
                transcribe_kwargs["language"] = language

            result = model.transcribe(audio, **transcribe_kwargs)
            detected_language = language or result.get("language", "en")

            # 2. Align for word-level timestamps
            align_model, align_metadata = whisperx.load_align_model(
                language_code=detected_language,
                device=self.device,
            )
            result = whisperx.align(
                result["segments"],
                align_model,
                align_metadata,
                audio,
                self.device,
                return_char_alignments=False,
            )
            del align_model
            gc.collect()

            # 3. Optionally diarize using WhisperX's pipeline (ffmpeg-based, no torchcodec)
            if diarize and hf_token:
                try:
                    from whisperx.diarize import DiarizationPipeline, assign_word_speakers

                    diarize_pipeline = DiarizationPipeline(
                        token=hf_token,
                        device=self.device,
                    )
                    diarize_segments = diarize_pipeline(audio)
                    result = assign_word_speakers(diarize_segments, result)

                    del diarize_pipeline
                    gc.collect()
                except Exception as e:
                    print(f"Warning: diarization failed, returning transcript without speakers: {e}", file=sys.stderr)

            # 4. Convert to segment contract format (see segment_schema.json)
            segments = []
            for seg in result["segments"]:
                start_ms = int(math.floor(seg.get("start", 0) * 1000))
                end_ms = int(math.floor(seg.get("end", 0) * 1000))
                text = seg.get("text", "").strip()
                speaker = seg.get("speaker", "") if diarize else ""

                if text:
                    segments.append({
                        "start_time": start_ms,
                        "end_time": end_ms,
                        "content": text,
                        "speaker": speaker,
                    })

            # 5. Optionally polish transcript via LLM
            if polish:
                segments = _polish_segments(segments, context_summary)

            # 6. Optionally compact into paragraphs
            if compact and diarize:
                segments = _compact_segments(segments, compact_gap)

            return segments
        finally:
            os.unlink(audio_path)
