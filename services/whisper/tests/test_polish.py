from modal_whisper.llm import LLMClient
from modal_whisper.polish import Polisher

SPOKEN = {
    1000: "entao a gente fecha o escopo dessa fase toda na sexta feira que vem, tudo bem",
    2000: "eu mando o documento revisado antes disso pra voces olharem com calma",
    3000: "perfeito, ai a gente decide na reuniao seguinte se entra ou nao entra no piloto",
}

GOOD = (
    "<segment:1000>\nEntão a gente fecha o escopo dessa fase toda na sexta-feira que vem, tudo bem?\n</segment>\n"
    "<segment:2000>\nEu mando o documento revisado antes disso pra vocês olharem com calma.\n</segment>\n"
    "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
)


class FakeLLM(LLMClient):
    """Answers with canned responses, calling nothing. Records every ask."""

    def __init__(self, batch: list[str], retries: list[str] | None = None):
        self.batch = batch
        self.retries = list(retries or [])
        self.asks = 0

    def call_batch_iter(self, messages_list):
        yield from self.batch

    def call(self, messages):
        self.asks += 1
        return self.retries.pop(0) if self.retries else ""


def segments() -> list[dict]:
    return [
        {"start_time": ts, "end_time": ts + 1000, "content": text, "speaker": "SPEAKER_00"}
        for ts, text in SPOKEN.items()
    ]


def polish(response: str, retries: list[str] | None = None):
    llm = FakeLLM([response], retries)
    polisher = Polisher(llm, context_summary="", language="pt")
    _, _, result = next(polisher.run_iter(segments()))
    return result, llm


def test_a_gutted_segment_costs_only_itself():
    result, _ = polish(
        "<segment:1000>\nEntão a gente fecha\n</segment>\n"
        "<segment:2000>\nEu mando o documento revisado antes disso pra vocês olharem com calma.\n</segment>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
    )

    assert result.answered
    assert result.refused == 1
    assert result.segments[0]["content"] == SPOKEN[1000]
    assert result.segments[1]["content"].startswith("Eu mando o documento revisado")


def test_a_segment_the_model_never_returned_stands_as_transcribed():
    result, _ = polish(
        "<segment:1000>\nEntão a gente fecha o escopo dessa fase toda na sexta-feira que vem, tudo bem?\n</segment>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
    )

    assert result.refused == 1
    assert result.segments[1]["content"] == SPOKEN[2000]


def test_an_answer_carrying_no_segments_is_asked_again():
    result, llm = polish("I'm sorry, I can't help with that.", retries=[GOOD])

    assert llm.asks == 1, "the chunk must be asked a second time"
    assert result.answered
    assert result.refused == 0
    assert result.segments[0]["content"].startswith("Então a gente fecha o escopo")


def test_asking_again_is_not_endless():
    result, llm = polish("nothing usable", retries=["still nothing usable"])

    assert llm.asks == 1
    assert not result.answered
    assert result.refused == 3
    assert [s["content"] for s in result.segments] == list(SPOKEN.values())


def test_a_second_ask_that_raises_leaves_the_chunk_as_transcribed():
    class Angry(FakeLLM):
        def call(self, messages):
            self.asks += 1
            raise RuntimeError("upstream said no")

    llm = Angry(["nothing usable"])
    polisher = Polisher(llm, context_summary="", language="pt")
    _, _, result = next(polisher.run_iter(segments()))

    assert llm.asks == 1
    assert not result.answered
    assert [s["content"] for s in result.segments] == list(SPOKEN.values())


def test_a_closing_tag_that_repeats_the_timestamp_is_still_a_segment():
    # What the model actually returns often enough to cost a whole chunk.
    result, llm = polish(
        "<segment:1000>\nEntão a gente fecha o escopo dessa fase toda na sexta-feira que vem, tudo bem?\n</segment:1000>\n"
        "<segment:2000>\nEu mando o documento revisado antes disso pra vocês olharem com calma.\n</segment:2000>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment:3000>"
    )

    assert llm.asks == 0, "nothing here needs asking again"
    assert result.answered
    assert result.refused == 0
    assert result.segments[0]["content"].startswith("Então a gente fecha o escopo")


def test_the_two_ways_of_closing_can_be_mixed():
    result, _ = polish(
        "<segment:1000>\nEntão a gente fecha o escopo dessa fase toda na sexta-feira que vem, tudo bem?\n</segment>\n"
        "<segment:2000>\nEu mando o documento revisado antes disso pra vocês olharem com calma.\n</segment:2000>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
    )

    assert result.refused == 0
