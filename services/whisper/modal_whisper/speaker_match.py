import math


class SpeakerMatcher:
    """Match new speaker embeddings against known speaker samples using cosine similarity."""

    def __init__(self, known_samples: list[tuple[int, str, list[float]]], threshold: float = 0.35):
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


def _cosine_distance(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(x * x for x in b))
    if norm_a == 0 or norm_b == 0:
        return 1.0
    return 1.0 - dot / (norm_a * norm_b)
