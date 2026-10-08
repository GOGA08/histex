# histex

An interactive picker for your shell command history: fuzzy-search the commands
you already ran, get them explained, and turn the useful ones into saved recipes
or ready-to-run scripts.

Built for **Windows + PowerShell**. It also *parses* bash/zsh history files
(`.bash_history`, `.zsh_history` incl. `#epoch` markers and zsh `: time:0;cmd`
lines), but only the Windows path is proven on a real machine so far.
A single Go binary, standard library only - the only external requirement is
[fzf](https://github.com/junegunn/fzf).

```
┌─────────────────────────────────────────────────────────────────────┐
│ ENTER explain | TAB mark | ^T save | ^O copy | ^P preview  663/663│
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
| Windows | **just the binary** - `histex.exe` is one self-contained file, no runtime needed |
| fzf | **required** - `winget install junegunn.fzf` (or `brew install fzf`) |
| tldr | optional, recommended - `winget install dbrgn.tealdeer`, then `tldr --update` |
| Go | 1.27+ - only to build from source; not needed to run the binary |

## Install

### Windows

`histex.exe` is a **single self-contained file** - download it and run it.
Nothing else has to be installed, and nothing is written next to it (your
recipes, scripts and cache live in `%APPDATA%\histex`).

**1. Download the binary**

```powershell
curl.exe -L -o histex.exe https://github.com/GOGA08/histex/releases/latest/download/histex.exe
```

or open the [releases page](https://github.com/GOGA08/histex/releases) and
download the `histex.exe` asset.

**2. Install fzf** - histex has no picker of its own, fzf draws the list:

```powershell
winget install junegunn.fzf
```

**3. Optional but recommended** - local tldr pages, so explanations work offline:

```powershell
winget install dbrgn.tealdeer
tldr --update
```

**4. Check the setup, then start it:**

```powershell
.\histex.exe --doctor      # tools, history, clipboard, recipes
.\histex.exe               # the picker: ENTER explains, CTRL-T saves
```

To run it simply as `histex` from anywhere, put `histex.exe` in a folder that
is on your `PATH` (for example `%USERPROFILE%\bin`). Open a **new** terminal
after the `winget install` lines - that is what refreshes `PATH`.

### Linux / macOS

### Build from source (developers)

Go 1.27+ is needed **only** for building - the result is the same
self-contained binary.

```powershell
git clone https://github.com/GOGA08/histex.git
cd histex
go build -o histex.exe .
powershell -ExecutionPolicy Bypass -File .\install.ps1
.\histex.exe
```

`install.ps1` checks fzf and tldr, builds `histex.exe` when it is missing,
writes the config file and runs `--doctor` plus `--self-test`. It never edits
`$PROFILE` - that stays your decision. The manual steps it performs are:

```powershell
winget install junegunn.fzf
winget install dbrgn.tealdeer     # optional but recommended
tldr --update
go build -o histex.exe .
.\histex.exe --init-config
.\histex.exe --doctor
```

## Keys

| key | action |
|---|---|
| `ENTER` | explain the current / marked command(s) |
| `TAB` / `SHIFT-TAB` | mark / unmark a command (to save several at once) |
| `CTRL-T` | save as a recipe (+ optional runnable script) |
| `CTRL-O` | copy the marked / current command(s) to the clipboard |
| `CTRL-P` (`CTRL-/` too) | toggle the preview pane (hidden by default) |
| `CTRL-R` | reload the list and toggle sorting (recent <-> most used) |
| `ESC` | cancel |

The preview pane is hidden by default so the list uses the full width - press
`CTRL-P` (or `CTRL-/`) to show the highlighted command in full, together with
the cached explanation when there is one. Start with it visible via
`--show-preview`, disable it entirely with `--no-preview`, or resize it with
the `preview_window` setting.

## Modes

```powershell
.\histex.exe                         # interactive picker (histex> prompt)
.\histex.exe --doctor                # health check: tools, history, clipboard
.\histex.exe --stats                 # most used commands and tools
.\histex.exe --browse                # search your saved recipes (recipes> prompt)
.\histex.exe --clean                 # delete entries from the history (auto backup)
.\histex.exe --explain "tar -xzf a.tgz"
.\histex.exe --pick                  # print only the selection (shell integration)
.\histex.exe --json                  # machine readable output
.\histex.exe --sort freq             # most used first
.\histex.exe --offline               # never touch the network
.\histex.exe --update-tldr           # refresh the local tldr page cache
.\histex.exe --install-snippets      # prompt integration + time/dir log
.\histex.exe --init-config           # write a config file
.\histex.exe --self-test             # offline self checks
```

## Explanations

Sources are tried in order (configurable, first hit wins):

```
local cache -> PowerShell Get-Help -> local tldr pages -> cheat.sh
```

`Get-Help` and `tldr` work offline, answers are cached in `%APPDATA%\histex\cache`, and
`--offline` guarantees nothing leaves your machine. Commands that look like they
contain a secret (`password`, `token`, ...) are never sent to the network.

`tldr` is the only source you may have to install, and it is the one that
explains non-PowerShell commands (`tar`, `git`, `docker`) while staying
offline. Without it histex still explains everything: PowerShell cmdlets come
from `Get-Help`, the rest from cheat.sh - which needs the network and is
usually shorter. That is why `--doctor` only warns about a missing `tldr`
instead of failing, and why it is listed as "optional, recommended".

## Saving recipes

`CTRL-T` asks for a title (with a suggestion), optional tags, and a format:

```
1 markdown      -> saved_recipes.md
2 +PowerShell   -> scripts/<slug>.ps1
3 +batch        -> scripts/<slug>.bat
4 +bash         -> scripts/<slug>.sh
```

Every recipe is also appended to `recipes.jsonl` for scripting.

`--browse` opens the recipes as their own library: a `recipes> ` prompt (never
the history picker) with a live preview of each recipe's stored commands.

## Configuration

`--init-config` writes `%APPDATA%\histex\config.json`:

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
