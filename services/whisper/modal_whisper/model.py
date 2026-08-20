import whisperx


class WhisperModel:
    """Long-lived: loaded once at container startup, shared by every request.

    Holds the Whisper model weights on GPU. A request contributes nothing to
    how the audio is decoded, so there is one model and no per-request reload.
    """

    def __init__(self, device: str, compute_type: str, model_name: str):
        self.device = device
        self.compute_type = compute_type
        self.model_name = model_name
        self._model = None

    def load(self):
        """Load the model. Called once at startup."""
        self._model = whisperx.load_model(
            self.model_name,
            self.device,
            compute_type=self.compute_type,
        )

    @property
    def model(self):
        """The model. Read-only access for TranscribeSession."""
        return self._model
