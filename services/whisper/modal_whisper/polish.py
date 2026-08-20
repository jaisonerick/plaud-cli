import re
import sys

from .llm import LLMClient
from .polish_guard import preserves_speech
from .prompts import load_prompt

_CHUNK_TARGET = 15
_SEGMENT_PATTERN = re.compile(r"<segment:(\d+)>\n?(.*?)\n?</segment>", re.DOTALL)


_LANGUAGE_NAMES = {
    "pt": "Brazilian Portuguese (pt-BR)",
    "en": "English",
    "es": "Spanish",
    "fr": "French",
    "de": "German",
    "it": "Italian",
}


class Polisher:
    """Fixes transcription errors via LLM, processing in parallel chunks."""

    def __init__(self, llm: LLMClient, context_summary: str, language: str = ""):
        self.llm = llm
        self.context_summary = context_summary
        self.language = language

    def _build_prompt(self) -> str:
        lang_name = _LANGUAGE_NAMES.get(self.language, self.language) if self.language else ""
        prompt = load_prompt("polish")
        if lang_name:
            prompt = prompt.replace("{language}", lang_name)
            prompt = prompt.replace("{language_instruction}", (
                f"CRITICAL: Do NOT translate the text. The output language MUST remain {lang_name}. "
                f"If the speech-to-text system produced text in the wrong language, convert it back to "
                f"{lang_name} — the speakers were speaking {lang_name}, not another language."
            ))
        else:
            prompt = prompt.replace("{language}", "the original language")
            prompt = prompt.replace("{language_instruction}",
                "Keep the text in the same language as the input. Do not translate.")
        prompt = prompt.replace("{context_summary}", self.context_summary)
        return prompt

    def run_iter(self, segments: list[dict]):
        """Yield (chunk_index, total_chunks, polished_chunk, kept) as each completes in order.

        `kept` counts the segments of that chunk left as transcribed.
        Used by transcribe_stream() for per-chunk progress events.
        """
        chunks = self._chunk(segments)
        total = len(chunks)
        print(f"Polishing transcript: {len(segments)} segments in {total} chunks (parallel)", file=sys.stderr)

        prompt = self._build_prompt()
        messages_list = [
            [
                {"role": "system", "content": prompt},
                {"role": "user", "content": self._format(chunk)},
            ]
            for chunk in chunks
        ]

        for i, response in enumerate(self.llm.call_batch_iter(messages_list)):
            polished, kept = self._parse(response.strip(), chunks[i])
            yield i, total, polished, kept

    @staticmethod
    def _format(chunk: list[dict]) -> str:
        lines = []
        for seg in chunk:
            lines.append(f"<segment:{seg['start_time']}>")
            lines.append(seg["content"])
            lines.append("</segment>")
        return "\n".join(lines)

    @staticmethod
    def _parse(text: str, chunk: list[dict]) -> tuple[list[dict], int]:
        """Apply the corrections that are still the speech, count the rest.

        A correction is matched by timestamp and judged on its own, so a
        segment the model gutted, merged away or never returned costs that
        segment and no other.
        """
        corrections = {
            int(ts): content.strip() for ts, content in _SEGMENT_PATTERN.findall(text)
        }

        kept = 0
        for seg in chunk:
            polished = corrections.get(seg["start_time"])
            if polished is not None and preserves_speech(seg["content"], polished):
                seg["content"] = polished
            else:
                kept += 1

        if kept:
            print(
                f"Polish: {kept}/{len(chunk)} segments kept as transcribed",
                file=sys.stderr,
            )

        return chunk, kept

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
