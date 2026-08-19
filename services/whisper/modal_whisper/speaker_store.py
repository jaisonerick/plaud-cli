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


def split_name(name: str) -> tuple[str, str]:
    """Split a full name into the two parts a shared store keeps.

    Anything past the second word is dropped: what used to arrive there was a
    company glued onto the name, and that has a column of its own now.
    """
    parts = name.split()
    if len(parts) < 2:
        raise NotFull(f"{name!r} is a first name; give a first and last name")
    return parts[0], parts[1]


class SpeakerStore:
    """The people this service recognises, and the voices it knows them by."""

    def __init__(self, db_path: str = DEFAULT_DB_PATH):
        self.db_path = db_path
        self._conn = sqlite3.connect(db_path, timeout=10)
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA foreign_keys=ON")
        self._ensure_tables()

    def _ensure_tables(self):
        self._conn.executescript("""
            CREATE TABLE IF NOT EXISTS audio_embeddings (
                audio_id TEXT NOT NULL,
                speaker_id TEXT NOT NULL,
                embedding BLOB NOT NULL,
                created_at TEXT NOT NULL,
                PRIMARY KEY (audio_id, speaker_id)
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

            -- How transcripts spell somebody, which only a person who knows
            -- them can answer, and which everybody then benefits from.
            CREATE TABLE IF NOT EXISTS aliases (
                spelling TEXT PRIMARY KEY,
                person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
                created_by TEXT NOT NULL,
                created_at TEXT NOT NULL
            );
        """)

    # -- people ----------------------------------------------------------

    def upsert_person(self, name: str, company: str, created_by: str) -> int:
        """Find the person by name, or record them. Returns their id."""
        first, last = split_name(name)
        if not company.strip():
            raise NotFull("a company is required, so a transcript can always name one")

        key = fold(f"{first} {last}")
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

    def rename_person(self, person_id: int, name: str, company: str) -> None:
        first, last = split_name(name)
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

    # -- aliases ---------------------------------------------------------

    def set_alias(self, spelling: str, person_id: int, created_by: str) -> None:
        now = datetime.now(timezone.utc).isoformat()
        self._conn.execute(
            """INSERT INTO aliases (spelling, person_id, created_by, created_at)
               VALUES (?, ?, ?, ?)
               ON CONFLICT(spelling) DO UPDATE SET person_id = excluded.person_id""",
            (fold(spelling), person_id, created_by, now),
        )
        self._conn.commit()

    def aliases(self) -> dict[str, str]:
        """Every spelling mapped to the full name it stands for."""
        rows = self._conn.execute("""
            SELECT a.spelling, p.first_name, p.last_name
            FROM aliases a JOIN people p ON p.id = a.person_id
        """).fetchall()
        return {
            row["spelling"]: f"{row['first_name']} {row['last_name']}"
            for row in rows
        }

    # -- per-recording embeddings ---------------------------------------

    def save_audio_embeddings(self, audio_id: str, embeddings: dict[str, list[float]]):
        now = datetime.now(timezone.utc).isoformat()
        self._conn.executemany(
            "INSERT OR REPLACE INTO audio_embeddings (audio_id, speaker_id, embedding, created_at) VALUES (?, ?, ?, ?)",
            [(audio_id, sid, _pack(vec), now) for sid, vec in embeddings.items()],
        )
        self._conn.commit()

    def get_audio_embeddings(self, audio_id: str) -> dict[str, list[float]]:
        rows = self._conn.execute(
            "SELECT speaker_id, embedding FROM audio_embeddings WHERE audio_id = ?",
            (audio_id,),
        ).fetchall()
        return {row["speaker_id"]: _unpack(row["embedding"]) for row in rows}

    def get_audio_speaker_info(self, audio_id: str, speaker_id: str) -> list[float] | None:
        row = self._conn.execute(
            "SELECT embedding FROM audio_embeddings WHERE audio_id = ? AND speaker_id = ?",
            (audio_id, speaker_id),
        ).fetchone()
        return _unpack(row["embedding"]) if row else None

    def close(self):
        self._conn.close()


def display(person) -> str:
    """How a person is written wherever anyone reads them."""
    return f"{person['first_name']} {person['last_name']} ({person['company']})"


def _pack(vec: list[float]) -> bytes:
    return struct.pack(f"{len(vec)}f", *vec)


def _unpack(blob: bytes) -> list[float]:
    count = len(blob) // 4
    return list(struct.unpack(f"{count}f", blob))
