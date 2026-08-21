import hashlib
import sqlite3
import struct
import unicodedata
from datetime import datetime, timezone

DEFAULT_DB_PATH = "/speakers/speakers.db"


class NotFull(Exception):
    """A name that does not identify one person to everybody."""


def fold(text: str) -> str:
    """Reduce a name to what two spellings of the same thing share.

    Must agree with Fold in internal/speaker/names.go: the client decides what
    to offer and this decides what is stored, and a disagreement shows up as a
    person who cannot be found under the name they were saved with.
    """
    stripped = unicodedata.normalize("NFKD", text.lower())
    kept = [
        " " if c in "-_" or c.isspace() else c
        for c in stripped
        if c.isalnum() or c.isspace() or c in "-_"
    ]
    return " ".join("".join(kept).split())


def split_name(name: str, surname_unknown: bool = False) -> tuple[str, str]:
    """Split a full name into the first name and everything that follows it.

    The rest is kept whole rather than trimmed to one word: "da Silva", "dos
    Santos" and "La O" are surnames, not leftovers. What used to be trimmed
    here was a company glued onto the name, and that has a column of its own.

    A surname may be absent, but only when the caller says so: the point of
    demanding one is to catch the "Amanda" typed without thinking, and an
    unknown surname is a different thing from an unconsidered one.
    """
    parts = name.split()
    if not parts:
        raise NotFull("a name is required")
    if len(parts) == 1:
        if not surname_unknown:
            raise NotFull(
                f"{name!r} is a first name; give a surname, or say it is unknown"
            )
        return parts[0], ""
    return parts[0], " ".join(parts[1:])


class SpeakerStore:
    """The people this service recognises, and the voices it knows them by."""

    def __init__(self, db_path: str = DEFAULT_DB_PATH):
        self.db_path = db_path
        self._conn = sqlite3.connect(db_path, timeout=10)
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA foreign_keys=ON")
        self._ensure_tables()
        self._ensure_voice_ids()
        self._keep_older_runs()
        # After the shape is settled, never before: an index names a column.
        self._conn.execute(
            "CREATE INDEX IF NOT EXISTS audio_embeddings_by_recording"
            " ON audio_embeddings (audio_id, current)"
        )
        self._conn.commit()

    def _ensure_tables(self):
        self._conn.executescript("""
            -- A voice is kept for good, and the id is what identifies it. A
            -- recording separated again leaves the voices of the run before in
            -- place, marked as no longer current, so a transcript written back
            -- then goes on saying whose voice it wrote down. What a label means
            -- is the current run's, and only that: naming SPEAKER_02 has to
            -- reach the voice a reader is looking at today.
            CREATE TABLE IF NOT EXISTS audio_embeddings (
                voice_id TEXT PRIMARY KEY,
                audio_id TEXT NOT NULL,
                speaker_id TEXT NOT NULL,
                embedding BLOB NOT NULL,
                created_at TEXT NOT NULL,
                current INTEGER NOT NULL DEFAULT 1
            );

            -- folded is UNIQUE so that a second spelling of one person cannot
            -- be created at all. Every other guard against that has been a
            -- convention, and conventions are what split them in the first place.
            CREATE TABLE IF NOT EXISTS people (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                folded TEXT NOT NULL UNIQUE,
                first_name TEXT NOT NULL,
                last_name TEXT NOT NULL,
                company TEXT NOT NULL,
                created_by TEXT NOT NULL,
                created_at TEXT NOT NULL
            );

            CREATE TABLE IF NOT EXISTS voices (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
                embedding BLOB NOT NULL,
                created_by TEXT NOT NULL,
                created_at TEXT NOT NULL
            );

        """)

    def _ensure_voice_ids(self):
        """Give every recorded voice an id of its own, once.

        A label is one run's numbering: transcribing again renumbers it, and a
        transcript written before that then points at somebody else. An id is
        not handed out twice, so a transcript whose voices were replaced finds
        nothing rather than the wrong person.
        """
        columns = {row["name"] for row in self._conn.execute("PRAGMA table_info(audio_embeddings)")}
        if "voice_id" not in columns:
            self._conn.execute("ALTER TABLE audio_embeddings ADD COLUMN voice_id TEXT")
        for row in self._conn.execute(
            "SELECT rowid, audio_id, speaker_id, created_at FROM audio_embeddings"
            " WHERE voice_id IS NULL OR voice_id = ''"
        ).fetchall():
            self._conn.execute(
                "UPDATE audio_embeddings SET voice_id = ? WHERE rowid = ?",
                (_voice_id(row["audio_id"], row["speaker_id"], row["created_at"]), row["rowid"]),
            )
        self._conn.commit()

    def _keep_older_runs(self):
        """Move a table that could hold one run per recording to one that holds
        every run, now that a voice has an id nobody confuses with another.

        The shape before had the label as half the key, so a recording
        separated again had to have its voices deleted first, and every
        transcript written from the run before lost the voices it named.
        """
        columns = {row["name"] for row in self._conn.execute("PRAGMA table_info(audio_embeddings)")}
        if "current" in columns:
            return

        self._conn.executescript("""
            CREATE TABLE audio_embeddings_kept (
                voice_id TEXT PRIMARY KEY,
                audio_id TEXT NOT NULL,
                speaker_id TEXT NOT NULL,
                embedding BLOB NOT NULL,
                created_at TEXT NOT NULL,
                current INTEGER NOT NULL DEFAULT 1
            );
            INSERT INTO audio_embeddings_kept
                (voice_id, audio_id, speaker_id, embedding, created_at, current)
                SELECT voice_id, audio_id, speaker_id, embedding, created_at, 1
                FROM audio_embeddings;
            DROP TABLE audio_embeddings;
            ALTER TABLE audio_embeddings_kept RENAME TO audio_embeddings;
            CREATE INDEX IF NOT EXISTS audio_embeddings_by_recording
                ON audio_embeddings (audio_id, current);
        """)
        self._conn.commit()

    # -- people ----------------------------------------------------------

    def upsert_person(
        self, name: str, company: str, created_by: str, surname_unknown: bool = False
    ) -> int:
        """Find the person by name, or record them. Returns their id."""
        first, last = split_name(name, surname_unknown)
        if not company.strip():
            raise NotFull("a company is required, so a transcript can always name one")

        key = fold(f"{first} {last}")
        clash = self._conn.execute(
            "SELECT company FROM people WHERE folded = ? AND company <> ?",
            (key, company.strip()),
        ).fetchone()
        if clash and not last:
            # Two people whose surname nobody wrote down are told apart by
            # nothing at all; this is the moment somebody has to look one up.
            raise NotFull(
                f"another {first} is already known, at {clash['company']} — "
                "a surname is needed to tell them apart"
            )
        now = datetime.now(timezone.utc).isoformat()
        self._conn.execute(
            """INSERT INTO people (folded, first_name, last_name, company, created_by, created_at)
               VALUES (?, ?, ?, ?, ?, ?)
               ON CONFLICT(folded) DO UPDATE SET company = excluded.company""",
            (key, first, last, company.strip(), created_by, now),
        )
        self._conn.commit()
        return self.person_id(f"{first} {last}")

    def person_id(self, name: str) -> int | None:
        row = self._conn.execute(
            "SELECT id FROM people WHERE folded = ?", (fold(name),)
        ).fetchone()
        return row["id"] if row else None

    def person(self, person_id: int) -> dict | None:
        row = self._conn.execute(
            "SELECT * FROM people WHERE id = ?", (person_id,)
        ).fetchone()
        return dict(row) if row else None

    def people(self) -> list[dict]:
        """Everybody known, with how many voices back them."""
        rows = self._conn.execute("""
            SELECT p.*, COUNT(v.id) AS voices
            FROM people p LEFT JOIN voices v ON v.person_id = p.id
            GROUP BY p.id
            ORDER BY p.first_name, p.last_name
        """).fetchall()
        return [dict(row) for row in rows]

    def rename_person(
        self, person_id: int, name: str, company: str, surname_unknown: bool = False
    ) -> None:
        first, last = split_name(name, surname_unknown)
        self._conn.execute(
            "UPDATE people SET folded = ?, first_name = ?, last_name = ?, company = ? WHERE id = ?",
            (fold(f"{first} {last}"), first, last, company.strip(), person_id),
        )
        self._conn.commit()

    def forget_person(self, person_id: int) -> int:
        cursor = self._conn.execute("DELETE FROM people WHERE id = ?", (person_id,))
        self._conn.commit()
        return cursor.rowcount

    # -- voices ----------------------------------------------------------

    def add_voice(self, person_id: int, embedding: list[float], created_by: str) -> int:
        now = datetime.now(timezone.utc).isoformat()
        self._conn.execute(
            "INSERT INTO voices (person_id, embedding, created_by, created_at) VALUES (?, ?, ?, ?)",
            (person_id, _pack(embedding), created_by, now),
        )
        self._conn.commit()
        row = self._conn.execute(
            "SELECT COUNT(*) AS n FROM voices WHERE person_id = ?", (person_id,)
        ).fetchone()
        return row["n"]

    def all_voices(self) -> list[tuple[int, str, list[float]]]:
        """Every voice as (person_id, display name, embedding), for matching."""
        rows = self._conn.execute("""
            SELECT v.person_id, p.first_name, p.last_name, p.company, v.embedding
            FROM voices v JOIN people p ON p.id = v.person_id
        """).fetchall()
        return [
            (row["person_id"], display(row), _unpack(row["embedding"]))
            for row in rows
        ]

    # -- per-recording embeddings ---------------------------------------

    def save_audio_embeddings(
        self, audio_id: str, embeddings: dict[str, list[float]]
    ) -> dict[str, str]:
        """Record the voices a recording was separated into.

        What came before is kept and marked as no longer current. The voices of
        an earlier run go on answering to their ids, which is how a transcript
        written back then still says whose voice it wrote down; what a label
        means is the current run's alone, because naming SPEAKER_02 has to
        reach the voice whoever is naming it is looking at.

        Returns the id given to each label, which is what a transcript carries
        so that it can still say whose voice it wrote down.
        """
        now = datetime.now(timezone.utc).isoformat()
        voice_ids = {label: _voice_id(audio_id, label, now) for label in embeddings}
        self._conn.execute(
            "UPDATE audio_embeddings SET current = 0 WHERE audio_id = ?", (audio_id,)
        )
        self._conn.executemany(
            "INSERT OR REPLACE INTO audio_embeddings"
            " (voice_id, audio_id, speaker_id, embedding, created_at, current)"
            " VALUES (?, ?, ?, ?, ?, 1)",
            [
                (voice_ids[label], audio_id, label, _pack(vec), now)
                for label, vec in embeddings.items()
            ],
        )
        self._conn.commit()
        return voice_ids

    def voices_of(self, audio_id: str, keys: list[str]) -> dict[str, tuple[str, list[float]]]:
        """The embedding each key stands for, by voice id or by label.

        Returns the id of the voice that answered next to its embedding, so a
        transcript that asked by a label can write the id down and stop
        depending on one run's numbering.

        A transcript written before voices had ids of their own says only
        SPEAKER_01, and that still answers as long as the recording has not
        been separated again since.
        """
        found = {}
        for key in keys:
            row = self._conn.execute(
                "SELECT voice_id, embedding FROM audio_embeddings WHERE voice_id = ?", (key,)
            ).fetchone()
            if row is None:
                row = self._conn.execute(
                    "SELECT voice_id, embedding FROM audio_embeddings"
                    " WHERE audio_id = ? AND speaker_id = ? AND current = 1",
                    (audio_id, key),
                ).fetchone()
            if row is not None:
                found[key] = (row["voice_id"], _unpack(row["embedding"]))
        return found

    def get_audio_embeddings(self, audio_id: str) -> dict[str, list[float]]:
        """The voices of the run that stands, which is what a label names."""
        rows = self._conn.execute(
            "SELECT speaker_id, embedding FROM audio_embeddings"
            " WHERE audio_id = ? AND current = 1",
            (audio_id,),
        ).fetchall()
        return {row["speaker_id"]: _unpack(row["embedding"]) for row in rows}

    def get_audio_speaker_info(self, audio_id: str, speaker_id: str) -> list[float] | None:
        row = self._conn.execute(
            "SELECT embedding FROM audio_embeddings"
            " WHERE audio_id = ? AND speaker_id = ? AND current = 1",
            (audio_id, speaker_id),
        ).fetchone()
        return _unpack(row["embedding"]) if row else None

    def close(self):
        self._conn.close()


def display(person) -> str:
    """How a person is written wherever anyone reads them."""
    name = " ".join(filter(None, [person["first_name"], person["last_name"]]))
    return f"{name} ({person['company']})"


def _voice_id(audio_id: str, speaker_id: str, created_at: str) -> str:
    """The id of one recorded voice, derived rather than drawn.

    Every container works out the same id for the same row, so an id handed to
    a transcript holds even if the write that filled it in was never published.
    Two runs of the same recording differ by when they were stored, which is
    what keeps a replaced voice from answering to the id of the one before.
    """
    seed = f"{audio_id}\x00{speaker_id}\x00{created_at}".encode()
    return "v_" + hashlib.sha256(seed).hexdigest()[:12]


def _pack(vec: list[float]) -> bytes:
    return struct.pack(f"{len(vec)}f", *vec)


def _unpack(blob: bytes) -> list[float]:
    count = len(blob) // 4
    return list(struct.unpack(f"{count}f", blob))
