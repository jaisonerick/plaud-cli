import json

import pytest

from modal_whisper.transcript_store import TranscriptStore


@pytest.fixture
def store(tmp_path):
    return TranscriptStore(str(tmp_path / "transcripts"))


def kept(text="olá"):
    return {"segments": [{"start_time": 0, "end_time": 1000, "content": text, "speaker": "SPEAKER_00"}]}


def test_a_transcript_comes_back_as_it_was_written(store):
    store.put("rec-1", kept())

    assert store.get("rec-1")["segments"][0]["content"] == "olá"


def test_a_recording_never_transcribed_has_nothing(store):
    assert store.get("rec-1") is None


def test_a_transcript_holding_no_segment_is_no_transcript(store):
    store.put("rec-1", {"segments": []})

    assert store.get("rec-1") is None


def test_a_file_too_broken_to_read_is_treated_as_absent(store):
    store.put("rec-1", kept())
    with open(store.path_of("rec-1"), "w") as f:
        f.write("{ not json")

    assert store.get("rec-1") is None


def test_writing_again_replaces_what_was_there(store):
    store.put("rec-1", kept("antes"))
    store.put("rec-1", kept("depois"))

    assert store.get("rec-1")["segments"][0]["content"] == "depois"


# The id names the file, so anything that is not a name is refused rather than
# reaching for a path of its own.
def test_an_id_that_is_not_a_name_is_refused(store):
    with pytest.raises(ValueError):
        store.path_of("../../etc/passwd")


def test_a_partial_write_is_never_read(store):
    store.put("rec-1", kept())
    leftovers = [f for f in __import__("os").listdir(store.directory) if f.endswith(".part")]

    assert leftovers == []


def segments_named():
    return [
        {"content": "a", "speaker": "Jaison Erick (NexaEdge)"},
        {"content": "b", "speaker": "SPEAKER_02"},
    ]


def test_a_transcript_is_stored_holding_labels_not_people():
    from modal_whisper.transcript_store import with_labels

    kept = with_labels(segments_named(), {"SPEAKER_00": "Jaison Erick (NexaEdge)", "SPEAKER_02": "SPEAKER_02"})

    assert [s["speaker"] for s in kept] == ["SPEAKER_00", "SPEAKER_02"]


def test_a_stored_transcript_is_handed_over_with_who_they_are_today():
    from modal_whisper.transcript_store import with_names

    given = with_names(
        [{"content": "a", "speaker": "SPEAKER_00"}, {"content": "b", "speaker": "SPEAKER_02"}],
        {"SPEAKER_00": "Jaison Erick (NexaEdge)", "SPEAKER_02": "Paulo Ionescu (CERC)"},
    )

    assert [s["speaker"] for s in given] == ["Jaison Erick (NexaEdge)", "Paulo Ionescu (CERC)"]
