import math
import re


class SegmentConverter:
    """Converts whisperx result to the segment contract format."""

    def __init__(self, diarized: bool = False):
        self.diarized = diarized

    def run(self, result: dict) -> list[dict]:
        segments = []
        for seg in result["segments"]:
            start_ms = int(math.floor(seg.get("start", 0) * 1000))
            end_ms = int(math.floor(seg.get("end", 0) * 1000))
            text = seg.get("text", "").strip()
            speaker = seg.get("speaker", "") if self.diarized else ""

            if text:
                text = _dedup_repeated_phrases(text)

            if text:
                segments.append({
                    "start_time": start_ms,
                    "end_time": end_ms,
                    "content": text,
                    "speaker": speaker,
                })

        return segments

    @staticmethod
    def dedup_segments(segments: list[dict]) -> list[dict]:
        """Apply repetition dedup to all segment contents."""
        for seg in segments:
            text = seg.get("content", "")
            if text:
                seg["content"] = _dedup_repeated_phrases(text)
        return [s for s in segments if s.get("content")]


def _dedup_repeated_phrases(text: str, max_repeats: int = 2) -> str:
    """Collapse runs of 3+ identical consecutive phrases into at most max_repeats.

    Catches hallucination patterns like:
      "Contrary. Contrary. Contrary. Contrary." -> "Contrary. Contrary."
      "Poucos. Poucos. Poucos. Poucos." -> "Poucos. Poucos."

    Works on sentence-ending punctuation boundaries (.!?) so normal speech
    like "sim, sim" or "é, é" is not affected.
    """
    # Split on sentence boundaries, keeping the delimiter attached.
    parts = re.split(r'(?<=[.!?])\s+', text)
    if len(parts) < 3:
        return text

    result = []
    repeat_count = 0
    prev = None

    for part in parts:
        normalized = part.strip().lower()
        if normalized == prev:
            repeat_count += 1
            if repeat_count < max_repeats:
                result.append(part)
        else:
            result.append(part)
            repeat_count = 0
            prev = normalized

    return " ".join(result)
