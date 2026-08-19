import os
import uuid
from dataclasses import dataclass, field
from typing import Iterator

from .compact import Compactor
from .context import ContextExtractor
from .diarize import Diarizer
from .llm import LLMClient
from .polish import Polisher
from .segments import SegmentConverter
from .speaker_match import SpeakerMatcher
from .speaker_store import DEFAULT_DB_PATH, SpeakerStore
from .model import WhisperModel
from .transcribe import TranscribeSession


@dataclass
class TranscribeOptions:
    """Per-request pipeline options. Immutable value object."""

    language: str = ""
    context_doc: str = ""
    recording_id: str = ""
    diarize: bool = False
    speaker_recognition: bool = False
    speaker_threshold: float = 0.35
    polish: bool = False
    compact: bool = False
    compact_gap: int = 2000


class TranscriptionPipeline:
    """Per-request pipeline. References long-lived resources, never mutates them.

    Usage:
        pipeline = TranscriptionPipeline(whisper_model, llm)
        for event in pipeline.transcribe_stream(audio_data, opts):
            # yield SSE event
    """

    def __init__(
        self,
        whisper_model: WhisperModel,
        llm: LLMClient,
        speaker_db_path: str = DEFAULT_DB_PATH,
    ):
        self._whisper_model = whisper_model
        self._llm = llm
        self._speaker_db_path = speaker_db_path

    def transcribe_stream(
        self, audio_data: bytes, opts: TranscribeOptions
    ) -> Iterator[dict]:
        """Execute pipeline as a generator, yielding progress events.

        All options come from `opts` — no shared mutable state.
        """
        # Keyed by the recording it came from, so a caller can name a speaker
        # later knowing only what it already knows. A minted id would have to
        # be written down somewhere, and the only somewhere is a caller's disk.
        audio_id = opts.recording_id or str(uuid.uuid4())

        # 1. Declare pipeline stages
        stages = []
        if opts.context_doc:
            stages.append({"id": "context", "label": "Extracting context"})
        stages.append({"id": "transcribe", "label": "Transcribing audio"})
        stages.append({"id": "align", "label": "Aligning timestamps"})
        if opts.diarize:
            stages.append({"id": "diarize", "label": "Diarizing speakers"})
            stages.append(
                {"id": "speaker_assign", "label": "Assigning speakers"}
            )
        stages.append(
            {"id": "segment_convert", "label": "Converting segments"}
        )
        if opts.speaker_recognition and opts.diarize:
            stages.append(
                {"id": "speaker_recognition", "label": "Recognizing speakers"}
            )
        if opts.compact and opts.diarize:
            stages.append({"id": "compact", "label": "Compacting segments"})
        if opts.polish:
            stages.append({"id": "polish", "label": "Polishing transcript"})

        yield {"type": "init", "stages": stages}

        # 2. Context extraction (lazy — runs inside the stream)
        hotwords = ""
        context_summary = ""
        if opts.context_doc:
            yield _update("context", "started")
            ctx = ContextExtractor(self._llm, opts.context_doc).run()
            context_summary = ctx.context_summary
            hotwords = ctx.hotwords
            n_hotwords = len([h for h in hotwords.split(",") if h.strip()])
            yield _update("context", "done", detail=f"{n_hotwords} hotwords")

        # 3. Transcription — per-request session
        #    Build initial_prompt from context to prime Whisper's decoder with
        #    domain terms, proper nouns, and language style.
        initial_prompt = ""
        if context_summary:
            initial_prompt = context_summary

        yield _update("transcribe", "started")
        session = TranscribeSession(
            self._whisper_model,
            hotwords=hotwords,
            initial_prompt=initial_prompt,
        )
        session.load_audio(audio_data)
        result = session.run(opts.language)
        seg_count = len(result.get("segments", []))
        detected_language = result.get("language", "")
        # Use forced language if provided, otherwise use detected
        effective_language = opts.language or detected_language
        yield _update("transcribe", "done", detail=f"{seg_count} segments, lang={effective_language}")

        # 4. Alignment
        yield _update("align", "started")
        result = session.align(result, language=opts.language)
        yield _update("align", "done")

        # 5. Diarization
        diarized = False
        speaker_embeddings = None
        if opts.diarize:
            yield _update("diarize", "started")
            hf_token = os.environ.get("HF_TOKEN", "")
            diarizer = Diarizer(session.device, hf_token)
            diarize_result = diarizer.run(session.audio)
            if diarize_result is not None:
                speaker_count = (
                    len(diarize_result.embeddings)
                    if diarize_result.embeddings
                    else 0
                )
                yield _update(
                    "diarize", "done", detail=f"{speaker_count} speakers"
                )

                # 6. Speaker assignment
                yield _update("speaker_assign", "started")
                result = Diarizer.assign_speakers(diarize_result, result)
                speaker_embeddings = diarize_result.embeddings
                diarized = True
                yield _update("speaker_assign", "done")
            else:
                yield _update("diarize", "done", detail="failed")
                yield _update("speaker_assign", "started")
                yield _update("speaker_assign", "done")

        # Free GPU memory from transcription, alignment, and diarization.
        # Audio and per-request model are no longer needed after this point.
        session.cleanup()

        # 7. Segment conversion
        yield _update("segment_convert", "started")
        segments = SegmentConverter(diarized).run(result)
        yield _update(
            "segment_convert", "done", detail=f"{len(segments)} segments"
        )

        # 8. Speaker recognition
        speaker_map = {}
        if diarized and speaker_embeddings:
            store = SpeakerStore(self._speaker_db_path)
            store.save_audio_embeddings(audio_id, speaker_embeddings)

            if opts.speaker_recognition:
                yield _update("speaker_recognition", "started")
                known = store.all_voices()
                matcher = SpeakerMatcher(known, opts.speaker_threshold)
                speaker_map = matcher.match(speaker_embeddings)
                matched = sum(1 for k, v in speaker_map.items() if k != v)
                yield _update(
                    "speaker_recognition",
                    "done",
                    detail=f"{matched} matched",
                )

                for seg in segments:
                    if seg["speaker"] in speaker_map:
                        seg["speaker"] = speaker_map[seg["speaker"]]
            else:
                speaker_map = {sid: sid for sid in speaker_embeddings}

            store.close()

        # 9. Compaction (before polishing so the LLM sees full paragraphs
        #    and can handle repetitions/hallucinations with context)
        if opts.compact and diarized:
            yield _update("compact", "started")
            segments = Compactor(opts.compact_gap).run(segments)
            yield _update(
                "compact", "done", detail=f"{len(segments)} paragraphs"
            )

        # 10. Polishing (with per-chunk progress)
        #     Pass the user's forced language directly. When set, the polisher
        #     will correct Whisper's output back to that language if Whisper
        #     auto-detected wrong. When empty, polishes in whatever language
        #     Whisper produced.
        if opts.polish:
            polisher = Polisher(self._llm, context_summary, language=opts.language)
            yield _update("polish", "started", detail="0 chunks")

            polished = []
            total_chunks = 0
            try:
                for i, total, chunk_result in polisher.run_iter(segments):
                    total_chunks = total
                    polished.extend(chunk_result)
                    yield _update(
                        "polish",
                        "progress",
                        detail=f"{i + 1}/{total} chunks",
                        progress={"current": i + 1, "total": total},
                    )
            except Exception as err:
                # Polishing runs last, on a transcript the GPU has already
                # finished. Anything the LLM does — refusing, timing out,
                # running out of credit — costs the wording, never the run.
                yield _update("polish", "done", detail=_polish_failure(err))
            else:
                segments = polished
                yield _update("polish", "done", detail=f"{total_chunks} chunks")

        # No embedding leaves this service. They are voices of people who never
        # agreed to be on anybody's laptop, and the store is shared, so every
        # caller would end up holding everyone else's.
        yield {
            "type": "result",
            "audio_id": audio_id,
            "segments": segments,
            "speakers": speaker_map,
        }

    def transcribe(self, audio_data: bytes, opts: TranscribeOptions) -> dict:
        """Execute pipeline, return final result dict.

        Delegates to transcribe_stream() — no duplicate logic.
        """
        result = None
        for event in self.transcribe_stream(audio_data, opts):
            if event["type"] == "result":
                result = event
        return {k: v for k, v in result.items() if k != "type"}


def _polish_failure(err: Exception) -> str:
    """Name a failed polish in the width a progress line has."""
    message = " ".join(str(err).split())
    if len(message) > 80:
        message = message[:80] + "..."
    return f"failed, keeping unpolished: {message}"


def _update(
    stage: str,
    status: str,
    detail: str | None = None,
    progress: dict | None = None,
) -> dict:
    """Build an update event dict."""
    event = {"type": "update", "stage": stage, "status": status}
    if detail is not None:
        event["detail"] = detail
    if progress is not None:
        event["progress"] = progress
    return event
