"""How much text a transcript carries for the second of speech it covers.

A decode that goes wrong does not raise. It returns fewer words for the same
audio, which reads as a quiet meeting unless something counts the difference.
Speech is the denominator rather than the recording, so a call that is mostly
silence is not mistaken for one that came back empty.
"""

from dataclasses import dataclass

_MIN_SPEECH_SECONDS = 60.0

# A floor, not a target: continuous speech in any language sits well above it,
# so what lands here is a decode that collapsed rather than a quiet speaker.
_SPARSE_BELOW = 5.0


@dataclass(frozen=True)
class Density:
    chars_per_second: float
    sparse: bool


def density(segments: list[dict]) -> Density | None:
    """None when there is too little speech for the rate to mean anything."""
    speech_seconds = sum(s["end_time"] - s["start_time"] for s in segments) / 1000
    if speech_seconds < _MIN_SPEECH_SECONDS:
        return None

    rate = sum(len(s["content"]) for s in segments) / speech_seconds
    return Density(chars_per_second=rate, sparse=rate < _SPARSE_BELOW)
