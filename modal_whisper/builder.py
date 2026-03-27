import os
import uuid

from .compact import Compactor
from .context import ContextExtractor
from .diarize import Diarizer
from .llm import LLMClient
from .polish import Polisher
from .segments import SegmentConverter
from .speaker_match import SpeakerMatcher
from .speaker_store import SpeakerStore
from .transcribe import Transcriber


class WhisperTranscriptionBuilder:
    """Builder for the transcription pipeline.

    Configure the pipeline, then call transcribe() to execute all steps.

    Usage:
        builder = WhisperTranscriptionBuilder(
            device="cuda",
            compute_type="float16",
            model_name="large-v3",
            llm_model="openrouter/anthropic/claude-sonnet-4",
            llm_api_key="sk-...",
        )
        builder.load()

        result = (
            builder
            .with_context(context_doc)
            .with_diarize()
            .with_speaker_recognition()
            .with_polish()
            .with_compact(max_gap_ms=2000)
            .transcribe(audio_data, language="pt")
        )
        # result = {"audio_id": "...", "segments": [...], "speakers": {...}}
    """

    def __init__(
        self,
        device: str,
        compute_type: str,
        model_name: str,
        llm_model: str,
        llm_api_key: str,
        llm_max_workers: int = 12,
        speaker_db_path: str = "/cache/speakers.db",
    ):
        self.device = device
        self._transcriber = Transcriber(device, compute_type, model_name)
        self._llm = LLMClient(
            model=llm_model,
            api_key=llm_api_key,
            max_workers=llm_max_workers,
        )
        self._speaker_db_path = speaker_db_path
        self._reset()

    def _reset(self):
        self._context_summary = ""
        self._hotwords = ""
        self._do_diarize = False
        self._do_polish = False
        self._do_compact = False
        self._compact_gap = 2000
        self._do_speaker_recognition = False
        self._speaker_threshold = 0.35

    def load(self) -> "WhisperTranscriptionBuilder":
        """Load the whisper model into memory."""
        self._transcriber.load()
        return self

    def with_context(self, context_doc: str) -> "WhisperTranscriptionBuilder":
        """Extract hotwords and context summary from a document."""
        ctx = ContextExtractor(self._llm, context_doc).run()
        self._context_summary = ctx.context_summary
        self._hotwords = ctx.hotwords
        return self

    def with_diarize(self) -> "WhisperTranscriptionBuilder":
        """Enable speaker diarization."""
        self._do_diarize = True
        return self

    def with_speaker_recognition(self, threshold: float = 0.35) -> "WhisperTranscriptionBuilder":
        """Enable speaker recognition against known speakers."""
        self._do_speaker_recognition = True
        self._speaker_threshold = threshold
        return self

    def with_polish(self) -> "WhisperTranscriptionBuilder":
        """Enable LLM transcript polishing."""
        self._do_polish = True
        return self

    def with_compact(self, max_gap_ms: int = 2000) -> "WhisperTranscriptionBuilder":
        """Enable paragraph compaction. Requires diarization."""
        self._do_compact = True
        self._compact_gap = max_gap_ms
        return self

    def transcribe(self, audio_data: bytes, language: str = "") -> dict:
        """Execute the configured pipeline and return result with audio_id."""
        audio_id = str(uuid.uuid4())

        self._transcriber.load_hotwords(self._hotwords)
        self._transcriber.load_audio(audio_data)
        result = self._transcriber.run(language)

        diarized = False
        speaker_embeddings = None
        speaker_map = {}

        if self._do_diarize:
            hf_token = os.environ.get("HF_TOKEN", "")
            diarizer = Diarizer(self.device, hf_token)
            diarize_result = diarizer.run(self._transcriber.audio)
            if diarize_result is not None:
                result = Diarizer.assign_speakers(diarize_result, result)
                speaker_embeddings = diarize_result.embeddings
                diarized = True

        segments = SegmentConverter(diarized).run(result)

        # Match speakers against known embeddings
        if diarized and speaker_embeddings:
            store = SpeakerStore(self._speaker_db_path)
            store.save_audio_embeddings(audio_id, speaker_embeddings)

            if self._do_speaker_recognition:
                known = store.get_all_known_speakers()
                matcher = SpeakerMatcher(known, self._speaker_threshold)
                speaker_map = matcher.match(speaker_embeddings)

                # Apply name mapping to segments
                for seg in segments:
                    if seg["speaker"] in speaker_map:
                        seg["speaker"] = speaker_map[seg["speaker"]]
            else:
                speaker_map = {sid: sid for sid in speaker_embeddings}

            store.close()

        if self._do_polish:
            segments = Polisher(self._llm, self._context_summary).run(segments)

        if self._do_compact and diarized:
            segments = Compactor(self._compact_gap).run(segments)

        self._reset()
        return {
            "audio_id": audio_id,
            "segments": segments,
            "speakers": speaker_map,
        }
