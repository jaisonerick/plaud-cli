from modal_whisper.llm import LLMClient
from modal_whisper.polish import Polisher

SPOKEN = {
    1000: "entao a gente fecha o escopo dessa fase toda na sexta feira que vem, tudo bem",
    2000: "eu mando o documento revisado antes disso pra voces olharem com calma",
    3000: "perfeito, ai a gente decide na reuniao seguinte se entra ou nao entra no piloto",
}


class FakeLLM(LLMClient):
    """Answers with a canned response per chunk, calling nothing."""

    def __init__(self, responses: list[str]):
        self.responses = responses

    def call_batch_iter(self, messages_list):
        yield from self.responses


def segments() -> list[dict]:
    return [
        {"start_time": ts, "end_time": ts + 1000, "content": text, "speaker": "SPEAKER_00"}
        for ts, text in SPOKEN.items()
    ]


def polish(response: str) -> tuple[list[dict], int]:
    polisher = Polisher(FakeLLM([response]), context_summary="", language="pt")
    _, _, polished, kept = next(polisher.run_iter(segments()))
    return polished, kept


def test_a_gutted_segment_costs_only_itself():
    polished, kept = polish(
        "<segment:1000>\nEntão a gente fecha\n</segment>\n"
        "<segment:2000>\nEu mando o documento revisado antes disso pra vocês olharem com calma.\n</segment>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
    )

    assert kept == 1
    assert polished[0]["content"] == SPOKEN[1000]
    assert polished[1]["content"].startswith("Eu mando o documento revisado")
    assert polished[2]["content"].startswith("Perfeito, aí a gente decide")


def test_a_segment_the_model_never_returned_stands_as_transcribed():
    polished, kept = polish(
        "<segment:1000>\nEntão a gente fecha o escopo dessa fase toda na sexta-feira que vem, tudo bem?\n</segment>\n"
        "<segment:3000>\nPerfeito, aí a gente decide na reunião seguinte se entra ou não entra no piloto.\n</segment>"
    )

    assert kept == 1
    assert polished[1]["content"] == SPOKEN[2000]
    assert polished[0]["content"].startswith("Então a gente fecha o escopo")


def test_an_answer_in_no_recognisable_shape_leaves_the_whole_chunk_as_transcribed():
    polished, kept = polish("I'm sorry, I can't help with that.")

    assert kept == 3
    assert [seg["content"] for seg in polished] == list(SPOKEN.values())
