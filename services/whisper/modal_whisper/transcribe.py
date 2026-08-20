import gc
import os
import tempfile

import torch
import whisperx

from .model import WhisperModel


def load_audio(audio_data: bytes):
    """Decode audio bytes into the 16kHz array every stage here works on."""
    with tempfile.NamedTemporaryFile(suffix=".mp3", delete=False) as f:
        f.write(audio_data)
        audio_path = f.name

    try:
        return whisperx.load_audio(audio_path)
    finally:
        os.unlink(audio_path)


class TranscribeSession:
    """Per-request: created in transcribe_stream(), garbage collected after.

    Holds the audio array and the model reference. Runs transcription and
    alignment for a single request.
    """

    def __init__(self, whisper_model: WhisperModel):
        self.device = whisper_model.device
        self._model = whisper_model.model
        self.audio = None

    def load_audio(self, audio_data: bytes):
        self.audio = load_audio(audio_data)

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
        """Release the audio and the GPU memory the request used."""
        self._model = None
        self.audio = None
        gc.collect()
        torch.cuda.empty_cache()
