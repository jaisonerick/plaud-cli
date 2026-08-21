"""The transcripts this service has made, kept so that none is made twice.

A transcription is minutes of GPU and, worse, it separates the voices again:
the labels are renumbered and every transcript written from the run before
points at voices that are gone. So the text is kept here, and a recording that
has been through this comes back rather than through the decoder.

What is kept is the transcript as it was decoded, with the labels the run gave
each voice. Who those labels are is a thing of today and is answered when the
transcript is handed over, never written down here: a name settled later has
to reach a transcript made earlier.
"""

import json
import os

DEFAULT_DIR = "/transcripts"


class TranscriptStore:
    def __init__(self, directory: str = DEFAULT_DIR):
        self.directory = directory
        os.makedirs(directory, exist_ok=True)

    def path_of(self, recording_id: str) -> str:
        return os.path.join(self.directory, f"{_safe(recording_id)}.json")

    def get(self, recording_id: str) -> dict | None:
        """The transcript on file, or None. A file too broken to read is not
        an answer: it is treated as absent, and the next run replaces it."""
        try:
            with open(self.path_of(recording_id), encoding="utf-8") as f:
                kept = json.load(f)
        except (FileNotFoundError, json.JSONDecodeError):
            return None
        return kept if kept.get("segments") else None

    def put(self, recording_id: str, transcript: dict) -> None:
        """Write it whole or not at all, so a reader never finds half of one."""
        target = self.path_of(recording_id)
        tmp = target + ".part"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(transcript, f, ensure_ascii=False)
        os.replace(tmp, target)


def with_labels(segments: list[dict], speakers: dict) -> list[dict]:
    """The segments as the run separated them, before anybody was named.

    What is kept has to survive the names changing, so a transcript is stored
    holding the label of each voice and never the person of the day.
    """
    label_of = {name: label for label, name in speakers.items()}
    return [{**seg, "speaker": label_of.get(seg.get("speaker", ""), seg.get("speaker", ""))}
            for seg in segments]


def with_names(segments: list[dict], speakers: dict) -> list[dict]:
    """The segments as a reader wants them, with whoever each voice is today."""
    return [{**seg, "speaker": speakers.get(seg.get("speaker", ""), seg.get("speaker", ""))}
            for seg in segments]


def _safe(recording_id: str) -> str:
    """A recording id names a file here, so anything that is not a name is
    refused rather than trimmed into one: trimming turns two ids into one."""
    if recording_id and all(c.isalnum() or c in "-_" for c in recording_id):
        return recording_id
    raise ValueError(f"{recording_id!r} is not usable as a name")
