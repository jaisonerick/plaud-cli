import re
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed

from .llm import llm_call

_CHUNK_TARGET = 50

_SYSTEM_PROMPT = (
    "You are a transcript post-processor. You receive transcript segments "
    "from a speech-to-text system in this format:\n\n"
    "<segment:START_TIME_MS>\ncontent\n</segment>\n\n"
    "Your job:\n"
    "- Fix obviously wrong words (misspellings, garbled words) using the meeting context\n"
    "- Fix proper nouns — names, companies, brands, technical terms\n"
    "- Improve punctuation and capitalization\n"
    "- Do NOT change meaning, remove content, add content, or summarize\n"
    "- Do NOT replace a word with a different word just because it seems more likely from context. "
    "Only fix words that are clearly misspelled or garbled by the speech-to-text system. "
    "If a word is a valid word but you're unsure if it's the right one, leave it as-is.\n"
    "- Do NOT merge or split segments — return the same number of segments\n"
    "- Keep the exact same <segment:TIMESTAMP> tags\n\n"
)

_SEGMENT_PATTERN = re.compile(r"<segment:(\d+)>\n?(.*?)\n?</segment>", re.DOTALL)


class Polisher:
    """Fixes transcription errors via LLM, processing in parallel chunks."""

    def __init__(self, context_summary: str):
        self.context_summary = context_summary

    def run(self, segments: list[dict]) -> list[dict]:
        chunks = self._chunk(segments)
        print(f"Polishing transcript: {len(segments)} segments in {len(chunks)} chunks (parallel)", file=sys.stderr)

        results = [None] * len(chunks)

        def process(idx, chunk):
            print(f"  Polishing chunk {idx+1}/{len(chunks)} ({len(chunk)} segments)...", file=sys.stderr)
            return idx, self._polish_chunk(chunk)

        with ThreadPoolExecutor(max_workers=len(chunks)) as pool:
            futures = [pool.submit(process, i, c) for i, c in enumerate(chunks)]
            for future in as_completed(futures):
                idx, polished_chunk = future.result()
                results[idx] = polished_chunk

        polished = []
        for chunk in results:
            polished.extend(chunk)
        return polished

    def _polish_chunk(self, chunk: list[dict]) -> list[dict]:
        messages = [
            {
                "role": "system",
                "content": (
                    _SYSTEM_PROMPT
                    + f"Meeting context: {self.context_summary}\n\n"
                    "Respond ONLY with the corrected segments in the same format. No explanation."
                ),
            },
            {"role": "user", "content": self._format(chunk)},
        ]
        raw = llm_call(messages)
        return self._parse(raw.strip(), chunk)

    @staticmethod
    def _format(chunk: list[dict]) -> str:
        lines = []
        for seg in chunk:
            lines.append(f"<segment:{seg['start_time']}>")
            lines.append(seg["content"])
            lines.append("</segment>")
        return "\n".join(lines)

    @staticmethod
    def _parse(text: str, chunk: list[dict]) -> list[dict]:
        matches = _SEGMENT_PATTERN.findall(text)

        if len(matches) != len(chunk):
            print(f"Warning: LLM returned {len(matches)} segments, expected {len(chunk)}. Using originals.", file=sys.stderr)
            return chunk

        corrections = {int(ts): content.strip() for ts, content in matches}
        for seg in chunk:
            if seg["start_time"] in corrections:
                seg["content"] = corrections[seg["start_time"]]

        return chunk

    @staticmethod
    def _chunk(segments: list[dict]) -> list[list[dict]]:
        if len(segments) <= _CHUNK_TARGET:
            return [segments]

        chunks = []
        current = []
        for i, seg in enumerate(segments):
            current.append(seg)
            if len(current) >= _CHUNK_TARGET:
                next_speaker = segments[i + 1].get("speaker") if i + 1 < len(segments) else None
                at_speaker_transition = seg.get("speaker") != next_speaker
                if at_speaker_transition or len(current) >= _CHUNK_TARGET + 10:
                    chunks.append(current)
                    current = []

        if current:
            chunks.append(current)
        return chunks
