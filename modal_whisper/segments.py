import math


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
                segments.append({
                    "start_time": start_ms,
                    "end_time": end_ms,
                    "content": text,
                    "speaker": speaker,
                })

        return segments
