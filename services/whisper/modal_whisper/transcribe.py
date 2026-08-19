import gc
import os
import tempfile

import torch
import whisperx

from .model import WhisperModel


class TranscribeSession:
    """Per-request: created in transcribe_stream(), garbage collected after.

    Holds the audio array and a (possibly customized) model reference.
    Runs transcription and alignment for a single request.
    """

    def __init__(
        self,
        whisper_model: WhisperModel,
        hotwords: str = "",
        initial_prompt: str = "",
        condition_on_previous_text: bool = False,
        beam_size: int = 5,
    ):
        self.device = whisper_model.device
        self._base_model = whisper_model.model
        self._model = whisper_model.with_asr_options(
            hotwords=hotwords,
            initial_prompt=initial_prompt,
            condition_on_previous_text=condition_on_previous_text,
            beam_size=beam_size,
        )
        self.audio = None

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
        """Transcribe loaded audio. Returns whisperx result dict with segments."""
        transcribe_kwargs = {"batch_size": 16}
        if language:
            transcribe_kwargs["language"] = language

        result = self._model.transcribe(self.audio, **transcribe_kwargs)
        return result

    def align(self, result: dict, language: str = "") -> dict:
        """Align word-level timestamps on transcription result."""
        detected_language = language or result.get("language", "en")

        align_model, align_metadata = whisperx.load_align_model(
            language_code=detected_language,
            device=self.device,
        )
        aligned = whisperx.align(
            result["segments"],
            align_model,
            align_metadata,
            self.audio,
            self.device,
            return_char_alignments=False,
        )
        del align_model, align_metadata
        gc.collect()
        torch.cuda.empty_cache()

        return aligned

    def cleanup(self):
        """Release GPU memory held by the per-request model and audio."""
        # If with_asr_options loaded a separate model, free it
        if self._model is not None and self._model is not self._base_model:
            del self._model
        self._model = None
        self.audio = None
        gc.collect()
        torch.cuda.empty_cache()
