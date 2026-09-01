import math

# What counts as the same voice. Transcription and any later question about a
# recording's voices must answer alike, so the number lives here only.
DEFAULT_THRESHOLD = 0.35


class SpeakerMatcher:
    """Match new speaker embeddings against known speaker samples using cosine similarity."""

    def __init__(self, known_samples: list[tuple[int, str, list[float]]], threshold: float = DEFAULT_THRESHOLD):
        self.known_samples = known_samples  # [(id, name, embedding), ...]
        self.threshold = threshold

    def match(self, new_embeddings: dict[str, list[float]]) -> dict[str, str]:
        """Match new speaker embeddings to known speakers.

        Compares each new speaker against ALL known samples. Multiple samples
        can share the same name (same person from different recordings, or
        different people with the same name — doesn't matter).

        Greedy best-match ensures each new speaker gets at most one name,
        and each name is assigned to at most one new speaker.
        """
        if not self.known_samples or not new_embeddings:
            return {sid: sid for sid in new_embeddings}

        # Compute all pairwise distances (new speaker vs every known sample)
        pairs = []
        for new_id, new_vec in new_embeddings.items():
            for sample_id, name, known_vec in self.known_samples:
                dist = _cosine_distance(new_vec, known_vec)
                if dist < self.threshold:
                    pairs.append((dist, new_id, name))

        # Greedy best-match: closest pairs first
        # Each new speaker gets at most one name, each name used at most once
        pairs.sort()
        used_new = set()
        used_names = set()
        mapping = {}

        for dist, new_id, name in pairs:
            if new_id in used_new or name in used_names:
                continue
            mapping[new_id] = name
            used_new.add(new_id)
            used_names.add(name)

        # Fill unmatched speakers with their original IDs
        for sid in new_embeddings:
            if sid not in mapping:
                mapping[sid] = sid

        return mapping


def ranked(
    embedding: list[float],
    known_samples: list[tuple[int, str, list[float]]],
    limit: int = 3,
) -> list[tuple[str, float]]:
    """The people this voice sounds most like, nearest first, once each.

    A person is as far away as their closest voice: several recordings of one
    person are several readings of the same thing, and the worst of them says
    nothing about whether this is them.
    """
    closest: dict[str, float] = {}
    for _, name, known in known_samples:
        distance = _cosine_distance(embedding, known)
        if name not in closest or distance < closest[name]:
            closest[name] = distance
    return sorted(closest.items(), key=lambda pair: pair[1])[:limit]


def nearest(
    embedding: list[float], known_samples: list[tuple[int, str, list[float]]]
) -> tuple[str, float] | None:
    """The person this voice sounds most like, and how far away they are.

    Answers for one voice on its own, unlike match(), which hands out each
    person once: a diarization that split one person into two labels is a real
    thing, and both halves are that person.
    """
    hits = ranked(embedding, known_samples, limit=1)
    return hits[0] if hits else None


def contradiction(
    embedding: list[float],
    claimed: list[list[float]],
    known_samples: list[tuple[int, str, list[float]]],
    threshold: float = DEFAULT_THRESHOLD,
) -> tuple[str, float, float] | None:
    """Who the store already holds this voice as, when that is not who it was
    given to. Returns (that person, how far they are, how far the claimed one
    is), or None when nothing contradicts the name.

    Both halves have to say so: the voice is nowhere near the person it was
    given to, and it is confidently somebody else. A voice matching nobody is a
    voice nobody has taught yet, and refusing that would leave a real person
    unnameable until somebody deleted a row.
    """
    if not claimed:
        return None
    hit = nearest(embedding, known_samples)
    if hit is None or hit[1] >= threshold:
        return None
    to_claimed = min(_cosine_distance(embedding, vector) for vector in claimed)
    if to_claimed < threshold:
        return None
    return hit[0], hit[1], to_claimed


def alike(
    voices: list[tuple[str, list[float]]], threshold: float = DEFAULT_THRESHOLD
) -> dict[str, list[tuple[str, float]]]:
    """Which of these voices are one voice, by the measure that decides whether
    a voice is somebody already known.

    Diarization separates one person into two labels as readily as it merges
    two people into one, and it does that per recording, so the same person
    turns up unnamed in several transcripts at once. Naming one of those then
    answers for all of them.
    """
    together: dict[str, list[tuple[str, float]]] = {key: [] for key, _ in voices}
    for i, (key, vector) in enumerate(voices):
        for other, other_vector in voices[i + 1 :]:
            distance = _cosine_distance(vector, other_vector)
            if distance < threshold:
                together[key].append((other, distance))
                together[other].append((key, distance))
    for held in together.values():
        held.sort(key=lambda pair: pair[1])
    return together


def _cosine_distance(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(x * x for x in b))
    if norm_a == 0 or norm_b == 0:
        return 1.0
    return 1.0 - dot / (norm_a * norm_b)
