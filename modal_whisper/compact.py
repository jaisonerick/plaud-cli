class Compactor:
    """Merges consecutive same-speaker segments into paragraphs."""

    def __init__(self, max_gap_ms: int = 2000):
        self.max_gap_ms = max_gap_ms

    def run(self, segments: list[dict]) -> list[dict]:
        if not segments:
            return segments

        compacted = [dict(segments[0])]
        for seg in segments[1:]:
            prev = compacted[-1]
            gap = seg["start_time"] - prev["end_time"]
            if seg["speaker"] and seg["speaker"] == prev["speaker"] and gap <= self.max_gap_ms:
                prev["end_time"] = seg["end_time"]
                prev["content"] += " " + seg["content"]
            else:
                compacted.append(dict(seg))

        return compacted
