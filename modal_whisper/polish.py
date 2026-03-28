import re
import sys

from .llm import LLMClient
from .prompts import load_prompt

_CHUNK_TARGET = 15
_SEGMENT_PATTERN = re.compile(r"<segment:(\d+)>\n?(.*?)\n?</segment>", re.DOTALL)


class Polisher:
    """Fixes transcription errors via LLM, processing in parallel chunks."""

    def __init__(self, llm: LLMClient, context_summary: str):
        self.llm = llm
        self.context_summary = context_summary

    def run(self, segments: list[dict]) -> list[dict]:
        chunks = self._chunk(segments)
        print(f"Polishing transcript: {len(segments)} segments in {len(chunks)} chunks (parallel)", file=sys.stderr)

        prompt = load_prompt("polish").replace("{context_summary}", self.context_summary)
        messages_list = [
            [
                {"role": "system", "content": prompt},
                {"role": "user", "content": self._format(chunk)},
            ]
            for chunk in chunks
        ]

        responses = self.llm.call_batch(messages_list)

        polished = []
        for i, (chunk, response) in enumerate(zip(chunks, responses)):
            polished.extend(self._parse(response.strip(), chunk))

        return polished

    def run_iter(self, segments: list[dict]):
        """Yield (chunk_index, total_chunks, polished_chunk) as each completes in order.

        Used by transcribe_stream() for per-chunk progress events.
        """
        chunks = self._chunk(segments)
        total = len(chunks)
        print(f"Polishing transcript: {len(segments)} segments in {total} chunks (parallel)", file=sys.stderr)

        prompt = load_prompt("polish").replace("{context_summary}", self.context_summary)
        messages_list = [
            [
                {"role": "system", "content": prompt},
                {"role": "user", "content": self._format(chunk)},
            ]
            for chunk in chunks
        ]

        for i, response in enumerate(self.llm.call_batch_iter(messages_list)):
            yield i, total, self._parse(response.strip(), chunks[i])

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
