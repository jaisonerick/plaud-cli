import sys

from .llm import LLMClient, strip_code_fences
from .prompts import load_prompt


class ContextUnusable(Exception):
    """The document was read and nothing came back to correct names with."""


class ContextExtractor:
    """Parses a context document into a summary the polisher can correct from.

    The summary comes back as prose rather than as a field in a JSON object.
    One string does not need a container, and asking a model for one is asking
    it to escape quotes it wrote itself: an unescaped one used to end the run
    before the audio had been touched.
    """

    def __init__(self, llm: LLMClient, context_doc: str):
        self.llm = llm
        self.context_doc = context_doc
        self.context_summary = ""

    def run(self) -> "ContextExtractor":
        print("Extracting context from document...", file=sys.stderr)
        self.context_summary = self._ask()
        if not self.context_summary:
            print("  Nothing came back; asking again.", file=sys.stderr)
            self.context_summary = self._ask()
        if not self.context_summary:
            raise ContextUnusable(
                "the context document produced no summary, twice over"
            )
        print(f"  Context: {self.context_summary[:100]}...", file=sys.stderr)
        return self

    def _ask(self) -> str:
        raw = self.llm.call(
            [
                {"role": "system", "content": load_prompt("context_extraction")},
                {"role": "user", "content": self.context_doc},
            ]
        )
        return strip_code_fences(raw or "").strip()
