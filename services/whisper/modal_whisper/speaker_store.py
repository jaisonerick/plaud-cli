import sqlite3
import struct
from datetime import datetime, timezone


DEFAULT_DB_PATH = "/speakers/speakers.db"


class SpeakerStore:
    """SQLite-backed storage for speaker embeddings on the Modal volume."""

    def __init__(self, db_path: str = DEFAULT_DB_PATH):
        self.db_path = db_path
        self._conn = sqlite3.connect(db_path, timeout=10)
        self._conn.execute("PRAGMA journal_mode=WAL")
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
            CREATE TABLE IF NOT EXISTS known_speakers (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                embedding BLOB NOT NULL,
                created_at TEXT NOT NULL
            );
        """)

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
        return {sid: _unpack(blob) for sid, blob in rows}

    def get_audio_speaker_info(self, audio_id: str, speaker_id: str) -> list[float] | None:
        row = self._conn.execute(
            "SELECT embedding FROM audio_embeddings WHERE audio_id = ? AND speaker_id = ?",
            (audio_id, speaker_id),
        ).fetchone()
        return _unpack(row[0]) if row else None

    def set_known_speaker(self, name: str, embedding: list[float]):
        """Add a new embedding sample for a known speaker."""
        now = datetime.now(timezone.utc).isoformat()
        self._conn.execute(
            "INSERT INTO known_speakers (name, embedding, created_at) VALUES (?, ?, ?)",
            (name, _pack(embedding), now),
        )
        self._conn.commit()

    def get_all_known_speakers(self) -> list[tuple[int, str, list[float]]]:
        """Return all known speaker samples as (id, name, embedding)."""
        rows = self._conn.execute("SELECT id, name, embedding FROM known_speakers").fetchall()
        return [(row_id, name, _unpack(blob)) for row_id, name, blob in rows]

    def get_known_speaker_counts(self) -> list[tuple[str, int]]:
        """Return each known speaker with how many samples back it, commonest first."""
        rows = self._conn.execute(
            "SELECT name, COUNT(*) FROM known_speakers GROUP BY name ORDER BY COUNT(*) DESC, name"
        ).fetchall()
        return [(name, count) for name, count in rows]

    def close(self):
        self._conn.close()


def _pack(vec: list[float]) -> bytes:
    return struct.pack(f"{len(vec)}f", *vec)


def _unpack(blob: bytes) -> list[float]:
    count = len(blob) // 4
    return list(struct.unpack(f"{count}f", blob))
