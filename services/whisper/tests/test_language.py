import pytest

from modal_whisper.language import Language, LanguageUnknown, decide, window_offsets


def test_the_majority_wins_and_reports_how_large_it_was():
    result = decide(["pt", "en", "pt", "pt", "pt"])

    assert result == Language(code="pt", agreement=0.8, samples=5)
    assert not result.uncertain


def test_a_split_vote_is_flagged_rather_than_acted_on_quietly():
    assert decide(["en", "pt", "pt", "es", "en"]).uncertain


def test_a_sample_that_heard_nothing_still_counts_against_agreement():
    result = decide(["pt", "", "", ""])

    assert result.code == "pt"
    assert result.agreement == 0.25
    assert result.uncertain


def test_hearing_nothing_anywhere_is_an_error_not_a_guess():
    with pytest.raises(LanguageUnknown):
        decide(["", "", ""])


def test_the_samples_span_the_recording():
    offsets = window_offsets(total=3600 * 16000, window=30 * 16000, count=5)

    assert len(offsets) == 5
    assert offsets[0] > 0, "the opening must not decide on its own"
    assert offsets[-1] + 30 * 16000 <= 3600 * 16000


def test_a_recording_shorter_than_one_window_is_sampled_once():
    assert window_offsets(total=10 * 16000, window=30 * 16000, count=5) == [0]
