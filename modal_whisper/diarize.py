import gc
import sys

from whisperx.diarize import DiarizationPipeline, assign_word_speakers


class DiarizeResult:
    """Result of diarization containing segments and optional embeddings."""

    def __init__(self, segments, embeddings: dict[str, list[float]] | None = None):
        self.segments = segments
        self.embeddings = embeddings


class Diarizer:
    """Assigns speaker labels to transcript segments using pyannote."""

    def __init__(self, device: str, hf_token: str):
        self.device = device
        self.hf_token = hf_token

    def run(self, audio) -> DiarizeResult | None:
        """Run diarization and return speaker segments with embeddings, or None on failure."""
        if not self.hf_token:
            return None

        try:
            pipeline = DiarizationPipeline(
                token=self.hf_token,
                device=self.device,
            )

            result = pipeline(audio, return_embeddings=True)

            # whisperx returns (df, embeddings_dict) when return_embeddings=True
            if isinstance(result, tuple):
                segments, embeddings = result
            else:
                segments = result
                embeddings = None

            del pipeline
            gc.collect()

            return DiarizeResult(segments, embeddings)
        except Exception as e:
            print(f"Warning: diarization failed: {e}", file=sys.stderr)
            return None

    @staticmethod
    def assign_speakers(diarize_result: DiarizeResult, transcript_result: dict) -> dict:
        """Assign speaker labels from diarization to transcript segments."""
        return assign_word_speakers(diarize_result.segments, transcript_result)
