import json
import sys

from .llm import LLMClient, strip_code_fences
from .prompts import load_prompt


class ContextExtractor:
    """Parses a context document into a summary the polisher can correct from."""

    def __init__(self, llm: LLMClient, context_doc: str):
        self.llm = llm
        self.context_doc = context_doc
        self.context_summary = ""

    def run(self) -> "ContextExtractor":
        print("Extracting context from document...", file=sys.stderr)
        prompt = load_prompt("context_extraction")
        messages = [
            {"role": "system", "content": prompt},
            {"role": "user", "content": self.context_doc},
        ]
        raw = self.llm.call(messages)
        result = json.loads(strip_code_fences(raw))
        self.context_summary = result.get("context_summary", "")
        print(f"  Context: {self.context_summary[:100]}...", file=sys.stderr)
        return self
