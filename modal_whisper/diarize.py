import gc
import sys

from whisperx.diarize import DiarizationPipeline, assign_word_speakers


class Diarizer:
    """Assigns speaker labels to transcript segments using pyannote."""

    def __init__(self, device: str, hf_token: str):
        self.device = device
        self.hf_token = hf_token

    def run(self, audio) -> dict | None:
        """Run diarization and return speaker segments, or None on failure."""
        if not self.hf_token:
            return None

        try:
            pipeline = DiarizationPipeline(
                token=self.hf_token,
                device=self.device,
            )
            result = pipeline(audio)

            del pipeline
            gc.collect()

            return result
        except Exception as e:
            print(f"Warning: diarization failed: {e}", file=sys.stderr)
            return None

    @staticmethod
    def assign_speakers(diarize_segments, transcript_result: dict) -> dict:
        """Assign speaker labels from diarization to transcript segments."""
        return assign_word_speakers(diarize_segments, transcript_result)
