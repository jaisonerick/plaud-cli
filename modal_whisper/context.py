import json
import sys

from .llm import llm_call, strip_code_fences

_SYSTEM_PROMPT = (
    "You are a pre-processing assistant for an audio transcription pipeline. "
    "Given a context document about a meeting or recording (this could be a prep file, "
    "agenda, notes, or any description), extract two things:\n\n"
    "1. **hotwords**: A comma-separated list of words the speech recognition model should "
    "prioritize. Extract from the document:\n"
    "   - Full names of participants (first and last)\n"
    "   - Company and organization names\n"
    "   - Product and brand names\n"
    "   - Technical terms, acronyms, and domain jargon\n"
    "   - Include common phonetic misspellings if obvious (e.g. for 'Jaison' include 'Jason,Gerson,Jorge')\n"
    "   Max 50 items.\n\n"
    "2. **context_summary**: A structured summary for a post-processing step that corrects "
    "transcription errors. Include:\n"
    "   - **People**: Full names and roles of all participants\n"
    "   - **Companies**: All companies/organizations mentioned and their relationship\n"
    "   - **Products**: Product names, tools, systems mentioned\n"
    "   - **Topic**: What the meeting is about — agenda, goals, key discussion points\n"
    "   Format as a compact paragraph covering all four areas.\n\n"
    'Respond ONLY with a JSON object: {"hotwords": "...", "context_summary": "..."}'
)


class ContextExtractor:
    """Parses a context document into hotwords and a structured context summary."""

    def __init__(self, context_doc: str):
        self.context_doc = context_doc
        self.hotwords = ""
        self.context_summary = ""

    def run(self) -> "ContextExtractor":
        print("Extracting context from document...", file=sys.stderr)
        messages = [
            {"role": "system", "content": _SYSTEM_PROMPT},
            {"role": "user", "content": self.context_doc},
        ]
        raw = llm_call(messages)
        result = json.loads(strip_code_fences(raw))
        self.context_summary = result.get("context_summary", "")
        self.hotwords = result.get("hotwords", "")
        print(f"  Context: {self.context_summary[:100]}...", file=sys.stderr)
        print(f"  Hotwords: {self.hotwords[:100]}...", file=sys.stderr)
        return self
