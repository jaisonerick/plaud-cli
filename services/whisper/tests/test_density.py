import pytest

from modal_whisper.density import density


def speech(chars_per_second: float, seconds: float) -> list[dict]:
    """One segment per second, each carrying the same amount of text."""
    return [
        {
            "start_time": i * 1000,
            "end_time": (i + 1) * 1000,
            "content": "a" * int(chars_per_second),
            "speaker": "SPEAKER_00",
        }
        for i in range(int(seconds))
    ]


def test_a_meeting_that_came_back_thin_is_flagged():
    result = density(speech(chars_per_second=1.6, seconds=3577))

    assert result is not None
    assert result.sparse


def test_a_meeting_transcribed_normally_is_not():
    result = density(speech(chars_per_second=14.8, seconds=3577))

    assert result is not None
    assert not result.sparse
    assert result.chars_per_second == pytest.approx(14.0)


def test_silence_between_the_words_does_not_count_against_the_rate():
    # Two minutes of speech spread across an hour, spoken at a normal rate.
    sparse_in_time = [
        {"start_time": i * 60_000, "end_time": i * 60_000 + 4000, "content": "a" * 60}
        for i in range(30)
    ]

    result = density(sparse_in_time)

    assert result is not None
    assert not result.sparse


def test_too_little_speech_to_judge_returns_nothing():
    assert density(speech(chars_per_second=1.0, seconds=30)) is None
    assert density([]) is None
