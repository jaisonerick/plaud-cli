import pytest

from modal_whisper.context import ContextExtractor, ContextUnusable
from modal_whisper.llm import LLMClient

DOC = "Reunião com a CERC sobre a esteira de pagamentos. Presentes: Éricles Bento, Zeni."


class FakeLLM(LLMClient):
    """Answers with canned responses, calling nothing. Records every ask."""

    def __init__(self, answers):
        self.answers = list(answers)
        self.asks = 0

    def call(self, messages):
        self.asks += 1
        return self.answers.pop(0) if self.answers else ""


def test_the_summary_is_taken_as_it_comes():
    # Quotes in the answer used to end the run: they broke the JSON it was
    # wrapped in, and the recording was never transcribed.
    written = 'Reunião da CERC. Grafar sempre "CERC", nunca "SERC".'

    ctx = ContextExtractor(FakeLLM([written]), DOC).run()

    assert ctx.context_summary == written


def test_a_fenced_answer_is_unwrapped():
    ctx = ContextExtractor(FakeLLM(["```\nReunião da CERC.\n```"]), DOC).run()

    assert ctx.context_summary == "Reunião da CERC."


def test_an_empty_answer_is_asked_again():
    llm = FakeLLM(["   ", "Reunião da CERC."])

    ctx = ContextExtractor(llm, DOC).run()

    assert llm.asks == 2
    assert ctx.context_summary == "Reunião da CERC."


def test_two_empty_answers_are_an_error_and_not_an_empty_context():
    llm = FakeLLM(["", ""])

    with pytest.raises(ContextUnusable):
        ContextExtractor(llm, DOC).run()

    assert llm.asks == 2
