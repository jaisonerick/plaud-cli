"""Which language a recording is in, decided over the whole of it.

Whisper decides from the opening thirty seconds, and the opening of a meeting
is people arriving: silence, a greeting, a stray word in another language. The
guess governs the whole file and does not degrade gracefully, because Whisper
translates rather than mis-spells, so a wrong one comes back as fluent prose in
a language nobody spoke.
"""

from collections import Counter
from dataclasses import dataclass

# Below this the samples disagreed enough that the answer is worth doubting
# out loud rather than acting on quietly.
_CLEAR_MAJORITY = 0.6


class LanguageUnknown(Exception):
    """No sample of the recording yielded a language."""


@dataclass(frozen=True)
class Language:
    code: str
    agreement: float
    samples: int

    @property
    def uncertain(self) -> bool:
        return self.agreement < _CLEAR_MAJORITY


def window_offsets(total: int, window: int, count: int) -> list[int]:
    """Where to take `count` samples so they span the recording.

    Each sits at the middle of its share of the audio, which keeps the opening
    from deciding alone and keeps the closing goodbyes out of it too.
    """
    if total <= window or count < 2:
        return [0]

    offsets = set()
    for i in range(count):
        centre = int(total * (i + 0.5) / count)
        offsets.add(max(0, min(centre - window // 2, total - window)))
    return sorted(offsets)


def decide(votes: list[str]) -> Language:
    """The language most samples heard, and how many of them agreed."""
    counted = Counter(v for v in votes if v)
    if not counted:
        raise LanguageUnknown("no sample of the audio yielded a language")

    code, hits = counted.most_common(1)[0]
    return Language(code=code, agreement=hits / len(votes), samples=len(votes))
