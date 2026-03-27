import gc
import os
import tempfile

import whisperx


class Transcriber:
    """Runs WhisperX transcription and word-level alignment."""

    def __init__(self, device: str, compute_type: str, model_name: str):
        self.device = device
        self.compute_type = compute_type
        self.model_name = model_name
        self._model = None
        self.audio = None

    def load(self):
        """Load the whisper model."""
        self._model = whisperx.load_model(
            self.model_name,
            self.device,
            compute_type=self.compute_type,
        )

    def load_hotwords(self, hotwords: str):
        """Reload the model with hotwords. No-op if hotwords is empty."""
        if not hotwords:
            return
        self._model = whisperx.load_model(
            self.model_name,
            self.device,
            compute_type=self.compute_type,
            asr_options={"hotwords": hotwords},
        )

    def load_audio(self, audio_data: bytes):
        """Write audio bytes to a temp file and load via whisperx."""
        with tempfile.NamedTemporaryFile(suffix=".mp3", delete=False) as f:
            f.write(audio_data)
            audio_path = f.name

        try:
            self.audio = whisperx.load_audio(audio_path)
        finally:
            os.unlink(audio_path)

    def run(self, language: str = "") -> dict:
        """Transcribe loaded audio and align word-level timestamps.

        Returns the whisperx result dict with aligned segments.
        """
        transcribe_kwargs = {"batch_size": 16}
        if language:
            transcribe_kwargs["language"] = language

        result = self._model.transcribe(self.audio, **transcribe_kwargs)
        detected_language = language or result.get("language", "en")

        align_model, align_metadata = whisperx.load_align_model(
            language_code=detected_language,
            device=self.device,
        )
        result = whisperx.align(
            result["segments"],
            align_model,
            align_metadata,
            self.audio,
            self.device,
            return_char_alignments=False,
        )
        del align_model
        gc.collect()

        return result
