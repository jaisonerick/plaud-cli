import sqlite3

import pytest

from modal_whisper.speaker_store import (
    NotFull,
    SpeakerStore,
    display,
    fold,
    split_name,
)


@pytest.fixture
def store(tmp_path):
    s = SpeakerStore(str(tmp_path / "speakers.db"))
    yield s
    s.close()


# These are the cases internal/speaker/names.go is tested against. The two must
# agree: the client decides what name to offer and this decides what is stored,
# so a disagreement is a person who cannot be found under the name they were
# saved with.
@pytest.mark.parametrize(
    "raw,expected",
    [
        ("Jaison Erick", "jaison erick"),
        ("  JAISON   erick ", "jaison erick"),
        ("Priscilla - Dinie", "priscilla dinie"),
        ("Cíntia", "cintia"),
        ("Mauricio", "mauricio"),
    ],
)
def test_fold_matches_the_client(raw, expected):
    assert fold(raw) == expected


def test_a_first_name_alone_is_refused():
    with pytest.raises(NotFull):
        split_name("Amanda")


def test_anything_past_the_second_word_is_dropped():
    # What used to arrive there was a company glued onto the name.
    assert split_name("Urias Hobaik - Afinz") == ("Urias", "Hobaik")


def test_a_person_needs_a_company(store):
    with pytest.raises(NotFull):
        store.upsert_person("Amanda Destro", "  ", "jaison@nexaedge.com")


def test_two_spellings_cannot_become_two_people(store):
    first = store.upsert_person("Jaison Erick", "NexaEdge", "jaison@nexaedge.com")
    again = store.upsert_person("  jaison   ERICK ", "NexaEdge", "someone@ppfxlabs.ai")
    assert first == again
    assert len(store.people()) == 1


def test_a_person_records_who_created_them(store):
    store.upsert_person("Aline Mazzoni", "Mevo", "jaison@ppfxlabs.ai")
    person = store.people()[0]
    assert person["created_by"] == "jaison@ppfxlabs.ai"
    assert person["company"] == "Mevo"


def test_voices_are_counted_per_person(store):
    person_id = store.upsert_person("Aline Mazzoni", "Mevo", "jaison@nexaedge.com")
    assert store.add_voice(person_id, [0.1, 0.2], "jaison@nexaedge.com") == 1
    assert store.add_voice(person_id, [0.3, 0.4], "jaison@nexaedge.com") == 2
    assert store.people()[0]["voices"] == 2


def test_a_voice_is_matched_against_a_person_written_in_full(store):
    person_id = store.upsert_person("Aline Mazzoni", "Mevo", "jaison@nexaedge.com")
    store.add_voice(person_id, [0.1, 0.2], "jaison@nexaedge.com")
    _, name, embedding = store.all_voices()[0]
    assert name == "Aline Mazzoni (Mevo)"
    assert embedding == pytest.approx([0.1, 0.2])


def test_forgetting_a_person_takes_their_voices(store):
    person_id = store.upsert_person("Aline Mazzoni", "Mevo", "jaison@nexaedge.com")
    store.add_voice(person_id, [0.1], "jaison@nexaedge.com")
    store.forget_person(person_id)
    assert store.all_voices() == []


def test_renaming_moves_the_voices_with_the_person(store):
    person_id = store.upsert_person("Mauricio Dinie", "Dinie", "jaison@nexaedge.com")
    store.add_voice(person_id, [0.5], "jaison@nexaedge.com")
    store.rename_person(person_id, "Mauricio Sobrenome", "Dinie")

    assert store.person_id("Mauricio Dinie") is None
    assert store.person_id("Mauricio Sobrenome") == person_id
    assert store.all_voices()[0][1] == "Mauricio Sobrenome (Dinie)"


def test_display_is_how_a_transcript_names_somebody():
    assert display(
        {"first_name": "Jaison", "last_name": "Erick", "company": "NexaEdge"}
    ) == "Jaison Erick (NexaEdge)"


def test_the_database_itself_refuses_a_duplicate(store):
    # Not a convention in the code above it: the constraint is what makes a
    # second spelling of one person impossible.
    store.upsert_person("Jaison Erick", "NexaEdge", "jaison@nexaedge.com")
    with pytest.raises(sqlite3.IntegrityError):
        store._conn.execute(
            "INSERT INTO people (folded, first_name, last_name, company, created_by, created_at)"
            " VALUES ('jaison erick', 'Jaison', 'Erick', 'Outra', 'x@y.com', 'now')"
        )
