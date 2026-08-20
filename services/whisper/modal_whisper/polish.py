import re
import sys
from dataclasses import dataclass

from .llm import LLMClient
from .polish_guard import preserves_speech
from .prompts import load_prompt

_CHUNK_TARGET = 15
# The closing tag is accepted with the timestamp repeated in it. Asking for
# the same tags back is read as an instruction to mirror the whole opening
# one, and a chunk closed that way carried no segments at all: fifteen
# openings, no closings, and the speech left as the recogniser wrote it.
_SEGMENT_PATTERN = re.compile(r"<segment:(\d+)>\n?(.*?)\n?</segment(?::\d+)?>", re.DOTALL)


_LANGUAGE_NAMES = {
    "pt": "Brazilian Portuguese (pt-BR)",
    "en": "English",
    "es": "Spanish",
    "fr": "French",
    "de": "German",
    "it": "Italian",
}


@dataclass(frozen=True)
class Polished:
    """What one chunk came back as.

    `refused` counts the segments left as transcribed. `answered` separates the
    two reasons that happens: a correction the guard would not take, and an
    answer that carried no segments at all, which is a failed call rather than
    a verdict on the speech.
    """

    segments: list[dict]
    refused: int
    answered: bool


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
        """Yield (chunk_index, total_chunks, Polished) as each completes in order.

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
            result = self._parse((response or "").strip(), chunks[i])
            if not result.answered:
                result = self._ask_again(prompt, chunks[i])
            yield i, total, result

    def _ask_again(self, prompt: str, chunk: list[dict]) -> "Polished":
        """One more try for a chunk whose answer carried no segments.

        Out of a batch of parallel calls one comes back as prose, or as
        nothing, often enough to matter, and letting that stand costs a whole
        stretch of the meeting rather than a wording.
        """
        print(
            f"Polish: asking again for {len(chunk)} segments whose answer carried none",
            file=sys.stderr,
        )
        try:
            response = self.llm.call(
                [
                    {"role": "system", "content": prompt},
                    {"role": "user", "content": self._format(chunk)},
                ]
            )
        except Exception as err:
            print(f"Polish: the second try failed too: {err}", file=sys.stderr)
            return Polished(segments=chunk, refused=len(chunk), answered=False)
        return self._parse((response or "").strip(), chunk)

    @staticmethod
    def _format(chunk: list[dict]) -> str:
        lines = []
        for seg in chunk:
            lines.append(f"<segment:{seg['start_time']}>")
            lines.append(seg["content"])
            lines.append("</segment>")
        return "\n".join(lines)

    @staticmethod
    def _parse(text: str, chunk: list[dict]) -> "Polished":
        """Apply the corrections that are still the speech, count the rest.

        A correction is matched by timestamp and judged on its own, so a
        segment the model gutted, merged away or never returned costs that
        segment and no other.
        """
        matches = _SEGMENT_PATTERN.findall(text)
        if not matches:
            # The counts are the diagnosis: an answer opening segments it never
            # closes is a different fault from one that answered in prose, and
            # the failure is intermittent enough that the next occurrence is
            # the only chance to tell them apart.
            print(
                f"Polish: the answer carried no segments "
                f"({len(text)} chars, {text.count('<segment:')} opened, "
                f"{text.count('</segment>')} closed): {text[:400]!r}",
                file=sys.stderr,
            )
            return Polished(segments=chunk, refused=len(chunk), answered=False)

        corrections = {int(ts): content.strip() for ts, content in matches}

        refused = 0
        for seg in chunk:
            polished = corrections.get(seg["start_time"])
            if polished is not None and preserves_speech(seg["content"], polished):
                seg["content"] = polished
            else:
                refused += 1

        if refused:
            print(
                f"Polish: {refused}/{len(chunk)} segments kept as transcribed",
                file=sys.stderr,
            )

        return Polished(segments=chunk, refused=refused, answered=True)

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
