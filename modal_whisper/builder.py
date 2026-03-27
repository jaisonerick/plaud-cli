import os

from .compact import Compactor
from .context import ContextExtractor
from .diarize import Diarizer
from .polish import Polisher
from .segments import SegmentConverter
from .transcribe import Transcriber


class WhisperTranscriptionBuilder:
    """Builder for the transcription pipeline.

    Configure the pipeline, then call transcribe() to execute all steps.

    Usage:
        builder = WhisperTranscriptionBuilder(device="cuda", compute_type="float16", model_name="large-v3")
        builder.load()

        segments = (
            builder
            .with_context(context_doc)
            .with_diarize()
            .with_polish()
            .with_compact(max_gap_ms=2000)
            .transcribe(audio_data, language="pt")
        )
    """

    def __init__(self, device: str, compute_type: str, model_name: str):
        self.device = device
        self.compute_type = compute_type
        self.model_name = model_name
        self._transcriber = Transcriber(device, compute_type, model_name)
        self._reset()

    def _reset(self):
        self._context_summary = ""
        self._hotwords = ""
        self._do_diarize = False
        self._do_polish = False
        self._do_compact = False
        self._compact_gap = 2000

    def load(self) -> "WhisperTranscriptionBuilder":
        """Load the whisper model into memory."""
        self._transcriber.load()
        return self

    def with_context(self, context_doc: str) -> "WhisperTranscriptionBuilder":
        """Extract hotwords and context summary from a document."""
        ctx = ContextExtractor(context_doc).run()
        self._context_summary = ctx.context_summary
        self._hotwords = ctx.hotwords
        return self

    def with_diarize(self) -> "WhisperTranscriptionBuilder":
        """Enable speaker diarization."""
        self._do_diarize = True
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

    def transcribe(self, audio_data: bytes, language: str = "") -> list[dict]:
        """Execute the configured pipeline and return segments."""
        self._transcriber.load_hotwords(self._hotwords)
        self._transcriber.load_audio(audio_data)
        result = self._transcriber.run(language)

        diarized = False
        if self._do_diarize:
            hf_token = os.environ.get("HF_TOKEN", "")
            diarizer = Diarizer(self.device, hf_token)
            diarize_segments = diarizer.run(self._transcriber.audio)
            if diarize_segments is not None:
                result = Diarizer.assign_speakers(diarize_segments, result)
                diarized = True

        segments = SegmentConverter(diarized).run(result)

        if self._do_polish:
            segments = Polisher(self._context_summary).run(segments)

        if self._do_compact and diarized:
            segments = Compactor(self._compact_gap).run(segments)

        self._reset()
        return segments
