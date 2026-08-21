from modal_whisper.speech import holds_speech, keep_speech


def segment(text, start, end, score):
    return {"text": text, "start": start, "end": end, "avg_logprob": score}


def test_a_window_the_decoder_would_not_stand_behind_is_dropped():
    result = {"segments": [segment("olá", 0, 2, -0.2), segment("...", 2, 4, -1.4)]}

    kept, dropped = keep_speech(result)

    assert [s["text"] for s in kept["segments"]] == ["olá"]
    assert dropped == [-1.4]


def test_a_transcript_from_before_the_scores_is_left_alone():
    result = {"segments": [{"text": "olá", "start": 0, "end": 2}]}

    kept, dropped = keep_speech(result)

    assert len(kept["segments"]) == 1
    assert dropped == []


# Six seconds of a microphone being put down came back as a confident
# "Thank you." at -0.69, where speech sits around -0.1.
def test_a_recording_that_never_rises_above_the_floor_holds_no_speech():
    result = {"segments": [segment(" Thank you.", 4.0, 6.0, -0.69)]}

    assert holds_speech(result) is False


def test_a_short_recording_of_real_speech_holds_speech():
    result = {"segments": [segment("Fala, Fabio, tudo bem?", 0.0, 3.0, -0.09)]}

    assert holds_speech(result) is True


# A meeting has passages the decoder finds hard, and those are hard rather
# than imagined: only a recording with almost no speech is judged this way.
def test_a_meeting_is_never_judged_on_its_hardest_passage():
    result = {
        "segments": [segment("...", start, start + 5, -0.8) for start in range(0, 60, 5)]
    }

    assert holds_speech(result) is True


def test_nothing_decoded_at_all_holds_no_speech():
    assert holds_speech({"segments": []}) is False
