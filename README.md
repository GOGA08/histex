# histex

An interactive picker for your shell command history: fuzzy-search the commands
you already ran, get them explained, and turn the useful ones into saved recipes
or ready-to-run scripts.

Built for **Windows + PowerShell**, works with **bash/zsh** history too. One
file, Python standard library only - the only external requirement is
[fzf](https://github.com/junegunn/fzf).

```
┌─────────────────────────────────────────────────────────────────────┐
│ ENTER: explain | TAB: mark | CTRL-T: save                     663/663│
│ histex> docker                                                      │
│ > docker compose up -d                                              │
│   docker ps -a                                                      │
│   docker system prune -af                                           │
└─────────────────────────────────────────────────────────────────────┘
```

## Why

Your history is full of solutions you already found. histex turns it into a
browsable, explainable knowledge base:

- **find** - fzf over your deduplicated history (newest first, or most used first)
- **understand** - explanations from local `tldr` pages, PowerShell `Get-Help`
  and cheat.sh, tried in order, cached, and partly available offline
- **keep** - save the commands that worked as a Markdown recipe, and optionally
  generate a runnable `.ps1`, `.bat` or `.sh` script
- **reuse** - copy anything to the clipboard without leaving the picker

## Requirements

| | |
|---|---|
| Python | 3.8+ (no third-party packages) |
| fzf | **required** - `winget install junegunn.fzf` (or `brew install fzf`) |
| tldr | optional, recommended - `winget install dbrgn.tealdeer`, then `tldr --update` |

## Install

```powershell
git clone https://github.com/GOGA08/histex.git
cd histex
winget install junegunn.fzf
winget install dbrgn.tealdeer     # optional but recommended
tldr --update
python histex.py
```

## Keys

| key | action |
|---|---|
| `ENTER` | explain the current / marked command(s) |
| `TAB` / `SHIFT-TAB` | mark / unmark a command (to save several at once) |
| `CTRL-T` | save as a recipe (+ optional runnable script) |
| `CTRL-O` | copy the marked / current command(s) to the clipboard |
| `CTRL-R` | reload the list and toggle sorting (recent <-> most used) |
| `ESC` | cancel |

The right-hand pane previews the highlighted command - its full text, since the
list truncates long lines - together with the cached explanation when there is
one. Hide it with `--no-preview`, or resize it with the `preview_window`
setting.

## Modes

```powershell
python histex.py                    # interactive picker
python histex.py --stats            # most used commands and tools
python histex.py --browse           # search your saved recipes
python histex.py --clean            # delete entries from the history (auto backup)
python histex.py --explain "tar -xzf a.tgz"
python histex.py --pick             # print only the selection (shell integration)
python histex.py --json             # machine readable output
python histex.py --sort freq        # most used first
python histex.py --offline          # never touch the network
python histex.py --update-tldr      # refresh the local tldr page cache
python histex.py --install-snippets # prompt integration + time/dir log
python histex.py --init-config      # write a config file
python histex.py --self-test        # offline self checks
```

## Explanations

Sources are tried in order (configurable, first hit wins):

```
local cache -> PowerShell Get-Help -> local tldr pages -> cheat.sh
```

`Get-Help` and `tldr` work offline, answers are cached in `~/.histex/cache`, and
`--offline` guarantees nothing leaves your machine. Commands that look like they
contain a secret (`password`, `token`, ...) are never sent to the network.

## Saving recipes

`CTRL-T` asks for a title (with a suggestion), optional tags, and a format:

```
1 markdown      -> saved_recipes.md
2 +PowerShell   -> scripts/<slug>.ps1
3 +batch        -> scripts/<slug>.bat
4 +bash         -> scripts/<slug>.sh
```

Every recipe is also appended to `recipes.jsonl` for scripting.

## Configuration

`--init-config` writes `~/.histex/config.json`:

| key | meaning |
|---|---|
| `explain_order` | order of the explanation sources |
| `sort` | `recent` or `freq` |
| `detail` | `short` or `full` |
| `network`, `cache`, `tldr`, `preview` | toggles |
| `exclude` | regexes removed from the picker (noise) |
| `secrets` | regexes that must never go online |
| `danger` | regexes that trigger a warning |
| `max_items`, `cache_ttl_hours` | limits |

## Notes

- The PowerShell history file records neither timestamps nor directories.
  `--install-snippets` writes a small profile snippet that adds a sidecar log,
  which enables `--today` and `--here`. It never edits `$PROFILE` for you.
- Nothing is ever executed for you - histex only explains, copies and saves.
- `saved_recipes.md`, `scripts/`, `recipes.jsonl` and `history_log.tsv` are
  git-ignored because they contain your own commands. See
  `saved_recipes.example.md` for the format.

## License

MIT - see [LICENSE](LICENSE).
