import pytest

from modal_whisper.polish_guard import preserves_speech

# What a 59-minute meeting actually came back as, in the run that opened
# issue #1: 894 characters of speech returned as 94, cut mid-word and spliced
# from fragments of later in the same turn.
TRUNCATED_ORIGINAL = (
    "Vindo você falar, Jason, me trouxe a lembrança de que a gente quer manter o "
    "conceito de human in the loop. Então, acho que o que você falou no último "
    "parece fazer sentido. Eu acho que existe, de fato, um conflito. É a mesma "
    "coisa que eu sou a pessoa que lidera o compras, ser a pessoa que aprova o "
    "pagamento."
)


def test_a_segment_returned_as_a_fraction_of_itself_is_refused():
    truncated = "Quando você falou, Jaison, me trouxe a lem O2C, O2C, O2C Bater uma análise crítica e falar ok,"

    assert not preserves_speech(TRUNCATED_ORIGINAL, truncated)


def test_boilerplate_the_speaker_never_said_is_refused():
    original = (
        "Eu acho que essa conversa aqui, para mim, o objetivo principal dessa "
        "conversa é a gente estar alinhado a algumas dessas decisões."
    )
    injected = (
        "Eu acho que essa conversa aqui, o objetivo principal. Clique no link "
        "abaixo na descrição do vídeo. A gente estar alinhado a algumas decisões."
    )

    assert not preserves_speech(original, injected)


def test_boilerplate_the_speaker_did_say_survives():
    said = "Clique no link abaixo que eu mandei no chat, é o contrato da CERC."

    assert preserves_speech(said, "Clique no link abaixo que eu mandei no chat, é o contrato da CERC.")


@pytest.mark.parametrize(
    "original,polished",
    [
        (
            "entao eu acho que a gente vai ter que atraiu a entrega do sitiou pra semana que vem",
            "Então eu acho que a gente vai ter que atrasar a entrega do CTO pra semana que vem.",
        ),
        (
            "e isso mesmo que voce falou sobre o processo de contas a pagar da cerc e do vex",
            "É isso mesmo que você falou sobre o processo de contas a pagar da CERC e do VEX.",
        ),
    ],
)
def test_the_corrections_polishing_exists_for_are_kept(original, polished):
    assert preserves_speech(original, polished)


@pytest.mark.parametrize("polished", ["Sim, sim.", "Tá bom.", "É."])
def test_a_short_answer_survives_being_punctuated(polished):
    assert preserves_speech(polished.lower().replace(".", ""), polished)


def test_a_collapsed_hallucination_loop_is_kept():
    looping = "Contrary. " * 12

    assert preserves_speech(looping, "Contrary. Contrary.")


def test_a_short_segment_is_refused_when_its_words_go_missing():
    assert not preserves_speech("Boa tarde, Amanda, tudo bem?", "Boa tarde Boa tarde.")


def test_two_segments_merged_into_one_is_refused():
    original = "Então a gente fecha o escopo dessa fase na sexta-feira, tudo bem?"
    merged = (
        "Então a gente fecha o escopo dessa fase na sexta-feira, tudo bem? "
        "Tudo bem. Eu mando o documento revisado antes disso pra vocês olharem "
        "com calma e a gente decide na reunião seguinte se entra ou não entra."
    )

    assert not preserves_speech(original, merged)


def test_an_empty_answer_is_refused():
    assert not preserves_speech("Bom dia a todos, vamos começar.", "   ")
