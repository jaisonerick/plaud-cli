import whisperx


class WhisperModel:
    """Long-lived: loaded once at container startup, never mutated after load().

    Holds the base Whisper model weights on GPU. Use with_asr_options() to get
    a customized model for a specific request — returns a new model instance
    without mutating the base.
    """

    def __init__(self, device: str, compute_type: str, model_name: str):
        self.device = device
        self.compute_type = compute_type
        self.model_name = model_name
        self._model = None

    # Anti-hallucination defaults applied to every transcription.
    # These go into asr_options at model load time.
    _BASE_ASR_OPTIONS = {
        # Reject segments where Whisper hallucinates during silence (>2s gap).
        "hallucination_silence_threshold": 2.0,
        # Lower than default (2.4) to reject repetitive outputs like
        # "Contrary. Contrary. Contrary." which have very high compression.
        "compression_ratio_threshold": 1.8,
    }

    def load(self):
        """Load base Whisper model (no hotwords). Called once at startup."""
        self._model = whisperx.load_model(
            self.model_name,
            self.device,
            compute_type=self.compute_type,
            asr_options=dict(self._BASE_ASR_OPTIONS),
        )

    @property
    def model(self):
        """The base model. Read-only access for TranscribeSession."""
        return self._model

    def with_asr_options(
        self,
        hotwords: str = "",
        initial_prompt: str = "",
        condition_on_previous_text: bool = False,
        beam_size: int = 5,
    ):
        """Return a model instance with custom ASR options. Does NOT mutate self.

        If no options differ from defaults, returns the base model (no reload).
        Otherwise loads a fresh model with the specified ASR options (~2-3s).
        """
        has_custom = (
            hotwords
            or initial_prompt
            or condition_on_previous_text
            or beam_size != 5
        )
        if not has_custom:
            return self._model

        # Start from base anti-hallucination options, then layer request-specific ones.
        asr_options = dict(self._BASE_ASR_OPTIONS)
        if hotwords:
            asr_options["hotwords"] = hotwords
        if initial_prompt:
            asr_options["initial_prompt"] = initial_prompt
        if condition_on_previous_text:
            asr_options["condition_on_previous_text"] = True
        if beam_size != 5:
            asr_options["beam_size"] = beam_size

        return whisperx.load_model(
            self.model_name,
            self.device,
            compute_type=self.compute_type,
            asr_options=asr_options,
        )

    def with_hotwords(self, hotwords: str):
        """Return a model instance with hotwords. Does NOT mutate self.

        If hotwords is empty, returns the base model (no reload).
        Otherwise loads a fresh model with ASR hotword options (~2-3s).

        Deprecated: use with_asr_options() instead.
        """
        return self.with_asr_options(hotwords=hotwords)
