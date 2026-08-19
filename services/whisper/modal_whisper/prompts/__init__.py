from pathlib import Path

# Default to the directory containing this file (works locally).
# On Modal, override via set_prompts_dir() to match add_local_dir remote_path.
_prompts_dir = Path(__file__).parent


def set_prompts_dir(path: str):
    global _prompts_dir
    _prompts_dir = Path(path)


def load_prompt(name: str) -> str:
    """Load a prompt file by name (without extension)."""
    return (_prompts_dir / f"{name}.md").read_text().strip()
