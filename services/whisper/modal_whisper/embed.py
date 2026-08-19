import numpy as np
import torch

_SAMPLE_RATE = 16000
_MIN_RANGE_MS = 1000
_MAX_SPEECH_SECONDS = 60

# The diarization pipeline embeds with this model, and a voice enrolled here is
# compared by cosine distance against the embeddings that pipeline produced.
# Any other model puts the two in different spaces, where nothing ever matches
# and nothing ever reports an error.
_EMBEDDING_MODEL = "pyannote/wespeaker-voxceleb-resnet34-LM"


class SpeakerEmbedder:
    """Turns chosen stretches of a recording into one voice embedding.

    The model loads on first use: a transcription that enrolls nobody never
    pays for it.
    """

    def __init__(self, device: str, hf_token: str):
        self._device = device
        self._hf_token = hf_token
        self._model = None

    def _loaded(self):
        if self._model is None:
            from pyannote.audio.pipelines.speaker_verification import (
                PretrainedSpeakerEmbedding,
            )

            self._model = PretrainedSpeakerEmbedding(
                _EMBEDDING_MODEL,
                device=torch.device(self._device),
                token=self._hf_token or None,
            )
        return self._model

    def embed(
        self, audio: np.ndarray, ranges: list[tuple[int, int]]
    ) -> list[float] | None:
        """Embed everything one speaker says in `ranges`, given in milliseconds.

        Returns None when the ranges hold too little speech to characterise a
        voice.
        """
        speech = _gather(audio, ranges)
        if speech is None:
            return None

        waveform = torch.from_numpy(speech).unsqueeze(0).unsqueeze(0)
        vector = self._loaded()(waveform)
        return [float(value) for value in np.asarray(vector).reshape(-1)]


def _gather(audio: np.ndarray, ranges: list[tuple[int, int]]) -> np.ndarray | None:
    """Concatenate the longest ranges until the duration budget is spent.

    Longest first because a backchannel "uh-huh" carries far less of a voice
    than a sentence does, and the budget is the only thing rationing them.
    """
    budget = _MAX_SPEECH_SECONDS * _SAMPLE_RATE
    total = len(audio)

    usable = sorted(
        (
            (start, end)
            for start, end in ranges
            if end - start >= _MIN_RANGE_MS
        ),
        key=lambda r: r[1] - r[0],
        reverse=True,
    )

    chunks = []
    taken = 0
    for start_ms, end_ms in usable:
        start = max(0, int(start_ms * _SAMPLE_RATE / 1000))
        end = min(total, int(end_ms * _SAMPLE_RATE / 1000))
        if end <= start:
            continue
        chunk = audio[start:end]
        if taken + len(chunk) > budget:
            chunk = chunk[: budget - taken]
        chunks.append(chunk)
        taken += len(chunk)
        if taken >= budget:
            break

    if taken < _MIN_RANGE_MS * _SAMPLE_RATE / 1000:
        return None
    return np.concatenate(chunks)


def cosine_distance(a: list[float], b: list[float]) -> float:
    left = np.asarray(a, dtype=np.float64)
    right = np.asarray(b, dtype=np.float64)
    norms = np.linalg.norm(left) * np.linalg.norm(right)
    if norms == 0:
        return 1.0
    return float(1.0 - np.dot(left, right) / norms)
