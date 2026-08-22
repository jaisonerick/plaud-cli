# The CLI owns the flow

A transcript reaches a repository through two programs today. The Go binary talks to Plaud and to the transcription service; a Python engine in the plugin decides where the file goes, what describes it, and what is kept about it. The decisions are the second program's, so every machine that wants them needs a Python, and every fix to them ships through the marketplace rather than through a release.

This moves the decisions into the binary. What is left in Python is the one thing Go cannot do for itself: put the binary on the machine and make the shell find it.

## What moves

| Today, in `plaud_hub.py` | After |
| :-- | :-- |
| reading `.plaud.json` | `internal/repo` |
| where a transcript lands, and what it is called | `internal/repo`, from templates |
| composing the repository's context with the call's | `plaud fetch` |
| `fetch` — probe, transcript, summary, catalog entry | `plaud fetch` |
| `doctor`, `config` | `plaud doctor`, `plaud config` |
| the catalog: `refresh`, `pull`, `set`, `status`, `query`, `gen-links` | `plaud catalog …` |
| installing the binary, PATH, Windows unblocking | stays in Python |

## Finding the repository

`.plaud.json` is looked for from the working directory upwards, and the directory holding it is the root. A git root is the fallback, and the working directory is the last one. Reading the git root first is what made a command run in `docs/` file its transcript against the wrong root, or against no configuration at all in a directory that is not a checkout.

## `plaud config`

Prints what was resolved and where it came from. A repository with no `.plaud.json` is not an error: it says so, and says that nothing declares where a transcript belongs here.

## `plaud doctor`

One report over both sign-ins, the release, and this repository. It stays a single command because the failure it exists to name is being signed in to one account and not the other, which otherwise surfaces halfway through a task.

## `plaud fetch <id>`

One recording, end to end: the transcript to where this repository puts it, the summary beside it when Plaud has one, the catalog entry when there is a catalog. It is `transcript --into` with the destination worked out rather than passed.

The context is composed rather than chosen. The repository's document holds the project's people and how their names are spelt; `--context` on the call holds who was in this room. Passing one must not drop the other. `--context-file` replaces the document, which is how a recording described by a paper of its own is fetched.

## `.plaud.json`

Everything today's file has, plus what a repository needs in order to stop deciding by hand:

```json
{
  "context": "contexto/briefing.md",
  "filing": ".agents/skills/meeting-notes/SKILL.md",
  "scratch": "workspace/plaud",
  "language": "pt",
  "name": "{date}-{slug}.md",
  "front_matter": { "type": "Transcript" },
  "profiles": {
    "cerc": {
      "tag": "CERC",
      "dest": "reunioes/{year}",
      "front_matter": { "client": "CERC" }
    }
  }
}
```

`name` and `dest` are templates over `{date}`, `{year}`, `{slug}`, `{id}`, `{time}`. A profile is a named set of those keys plus the tag that selects recordings for it, which is what `plaud sync --profile` walks.

`front_matter` is written above the `voices:` block, so a transcript arrives with what the repository files by rather than being edited into shape afterwards.

## `plaud sync --profile <name>`

Every recording carrying the profile's tag that this repository has not filed yet. Idempotent, because what says a recording is filed is the file: a transcript carries its recording id in its front matter, and a destination directory is scanned rather than remembered.

## The catalog

It stays a `catalog.jsonl` at the repository, git-tracked, and it stays the source of truth. The curation in it is about that repository — which project a recording belongs to, which note it became — so it is versioned with it, and moving it to an account-level store would separate it from the thing it describes.

The sqlite index goes. It exists to answer `query`, and it is rebuilt from the catalog by a command a person has to remember to run, which is a second copy that can be stale. `catalog list` filters in the CLI and prints JSON, which covers what the queries in use actually ask.
