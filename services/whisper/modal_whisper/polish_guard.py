"""Whether what the polisher got back is still the segment that was spoken.

Polishing asks an LLM to correct spelling, accents and punctuation. Nothing in
that request stops it returning a summary, half a sentence, or a line from its
own training data, and what it returns is read afterwards as a quotation.
"""

import re
import unicodedata

_MAX_LENGTH_RATIO = 2.0
_MIN_RETAINED = 0.65
_MAX_NOVEL = 0.4

_WORD = re.compile(r"\w+")


def _fold(text: str) -> str:
    decomposed = unicodedata.normalize("NFD", text.casefold())
    return "".join(c for c in decomposed if not unicodedata.combining(c))


def _words(text: str) -> set[str]:
    return set(_WORD.findall(_fold(text)))


# Whisper writes these over silence, having learnt them from subtitled video.
# The polisher can introduce one as readily as it can pass one through, so the
# test is whether it is new, not whether it is there.
_BOILERPLATE = tuple(
    _fold(phrase)
    for phrase in (
        "clique no link",
        "inscreva-se no canal",
        "não se esqueça de se inscrever",
        "legendas pela comunidade",
        "amara.org",
        "subscribe to the channel",
        "like and subscribe",
        "thanks for watching",
    )
)


def preserves_speech(original: str, polished: str) -> bool:
    """Whether a polished segment still says what the original said."""
    polished = polished.strip()
    if not polished or not original:
        return False

    # A ceiling and no floor: polishing is asked to collapse a hallucination
    # loop, so a segment coming back a tenth of its length can be correct,
    # while one coming back at twice is carrying words from somewhere else.
    if len(polished) > len(original) * _MAX_LENGTH_RATIO:
        return False

    if _introduces_boilerplate(original, polished):
        return False

    return _keeps_the_same_words(original, polished)


def _introduces_boilerplate(original: str, polished: str) -> bool:
    spoken = _fold(original)
    returned = _fold(polished)
    return any(p in returned and p not in spoken for p in _BOILERPLATE)


def _keeps_the_same_words(original: str, polished: str) -> bool:
    """Most of what was said survives, and little of what comes back is new.

    Words count once each, so the loop the polisher collapsed reads as the one
    word it was, while the sentence it invented reads as every word it added.
    """
    spoken = _words(original)
    returned = _words(polished)
    if not spoken or not returned:
        return False

    retained = len(spoken & returned) / len(spoken)
    novel = len(returned - spoken) / len(returned)
    return retained >= _MIN_RETAINED and novel <= _MAX_NOVEL
