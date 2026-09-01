from modal_whisper.speaker_match import (
    DEFAULT_THRESHOLD,
    alike,
    contradiction,
    nearest,
    ranked,
)

# Vectors chosen so the distances read off the page: identical is 0, and the
# angle between two of them is what separates one person from another.
JAISON = [1.0, 0.0, 0.0]
JAISON_AGAIN = [0.99, 0.14, 0.0]
AMANDA = [0.0, 1.0, 0.0]
STRANGER = [0.0, 0.0, 1.0]

KNOWN = [
    (1, "Jaison Erick (NexaEdge)", JAISON),
    (1, "Jaison Erick (NexaEdge)", JAISON_AGAIN),
    (2, "Amanda Silva (NexaEdge)", AMANDA),
]


def test_a_person_is_as_far_away_as_their_closest_voice():
    assert ranked(JAISON_AGAIN, KNOWN)[0][0] == "Jaison Erick (NexaEdge)"
    assert ranked(JAISON_AGAIN, KNOWN)[0][1] < 0.01


def test_each_person_is_offered_once_however_many_voices_they_have():
    offered = [name for name, _ in ranked(JAISON, KNOWN)]

    assert offered == ["Jaison Erick (NexaEdge)", "Amanda Silva (NexaEdge)"]


def test_nobody_known_means_no_answer():
    assert nearest(JAISON, []) is None
    assert ranked(JAISON, []) == []


def test_a_name_the_store_holds_for_somebody_else_is_contradicted():
    clash = contradiction(JAISON_AGAIN, [AMANDA], KNOWN)

    assert clash is not None
    theirs, distance, claimed_distance = clash
    assert theirs == "Jaison Erick (NexaEdge)"
    assert distance < DEFAULT_THRESHOLD <= claimed_distance


def test_a_voice_nobody_has_learned_contradicts_no_name():
    """Refusing here would leave a real person unnameable until a row was deleted."""
    assert contradiction(STRANGER, [AMANDA], KNOWN) is None


def test_naming_somebody_the_voice_already_matches_is_no_contradiction():
    assert contradiction(JAISON_AGAIN, [JAISON], KNOWN) is None


def test_a_person_with_no_voices_yet_contradicts_nothing():
    assert contradiction(JAISON_AGAIN, [], KNOWN) is None


def test_voices_that_are_one_voice_find_each_other():
    together = alike([("a", JAISON), ("b", JAISON_AGAIN), ("c", AMANDA)])

    assert [key for key, _ in together["a"]] == ["b"]
    assert [key for key, _ in together["b"]] == ["a"]
    assert together["c"] == []


def test_the_nearest_of_several_alike_comes_first():
    together = alike([("a", JAISON), ("b", [0.8, 0.6, 0.0]), ("c", JAISON_AGAIN)])

    assert [key for key, _ in together["a"]] == ["c", "b"]
