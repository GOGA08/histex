# Changelog

All notable changes to histex. No browser integration - terminal and files only.

## [Unreleased]

### Fixed
- `CTRL-R` (reload) no longer drops the `--today` / `--here` filter. The reload
  bind now forwards both flags (`--print-list --toggle-sort --today --here`) and
  `print_list` applies the sidecar filter again, so the reloaded list matches
  the list the picker opened with.
- `CTRL-R` in the recipe library no longer swaps the recipes for the shell
  history. The prepared recipe list now travels to the reload process through
  `HISTEX_RECIPES_LIST` (the same file hand-off the preview uses), so a reload
  shows the same titles again instead of the history.

### Changed
- CI runs `staticcheck` (pinned to `v0.8.1`) after `go vet`; the one finding it
  reported was simplified. CONTRIBUTING.md and the README list it alongside
  gofmt / vet / test. The self-test job pins Go to 1.27.1, because staticcheck
  v0.8.1 (latest) cannot read Go 1.27.2's stdlib export data yet.

## [2.4] - 2026-10-10

### Added
- fish history support: `$XDG_DATA_HOME/fish/fish_history` (default
  `~/.local/share/fish/fish_history`) is detected like the other shells and
  parsed as the YAML fish writes it, block form included, so multi-line
  commands survive. `--shell fish` picks it explicitly.
- `--install-snippets` writes `histex_profile.sh` on Linux/macOS: a `histex`
  picker function for bash and zsh (with the `bind -x` line for bash) plus a
  `PROMPT_COMMAND` / `precmd` hook that appends to the same `history_log.tsv`
  the PowerShell snippet writes - so `--today` and `--here` now work off
  Windows too.
- New explanation source `toolhelp`: the tool's own `--help` (`tar -xzf a.tgz`
  -> `tar --help`), offline, capped at 60 lines, 5 second timeout, no stdin and
  the pagers forced off. It is in the default `explain_order` between the local
  tldr pages and cheat.sh, and `"toolhelp": false` turns it off.
- `--stats --since WINDOW` counts the sidecar log instead of the history file:
  `90m`, `24h`, `7d`, `4w` or a date like `2026-10-01`.
- `--restore` copies the `.histex-backup` that `--clean` writes back over the
  history file, and says so when there is no backup to restore.
- `--stats` also reports whether `toolhelp` is on.

### Changed
- The default `explain_order` gained `toolhelp`; a config file that names its
  own order keeps it, so add `toolhelp` there to switch the new source on.
- Seven more self checks (60 total), plus `go test` table tests for the fish
  parser, the `--since` parser and the tool-help query.

## [2.3] - 2026-10-09

### Added
- Prebuilt binaries for Linux (amd64, arm64) and macOS (arm64, amd64) next to
  `histex.exe`. The release workflow builds and self-tests them on each
  platform, and the README documents the download, `chmod +x`, fzf per platform
  and the Windows-only sidecar log.
- `SHA256SUMS` on every release, so a download can be verified with
  `Get-FileHash`, `sha256sum` or `shasum -a 256`.
- Package manifests: `packaging/scoop/histex.json` (installs `histex.exe` and
  pulls fzf from the main bucket) and `packaging/winget/` for a later
  microsoft/winget-pkgs submission - the steps live in `packaging/README.md`.

### Changed
- The release workflow is a matrix (windows / ubuntu / macos): every runner
  builds its own binaries, self-tests the native one and checks that the
  reported version matches the tag before anything is published.
- `go test ./...` runs the same check list as `histex --self-test` (they can no
  longer drift apart), and CI runs it on Windows and Ubuntu.

## [2.2] - 2026-10-09

### Added
- The Windows release also ships `fzf.exe` (fzf 0.74.4, MIT - the license text
  is attached as `fzf-LICENSE.txt`). histex prefers an fzf sitting next to
  `histex.exe`, so downloading the release needs no `winget install`.

### Changed
- A config pattern that Go's regexp cannot compile (lookbehind, backreferences)
  is now reported on stderr instead of being dropped silently.
- An unreadable history file now prints `[x] could not read the history file:
  ...` and exits 1 in `--stats`, `--clean` and the picker, and a failed
  sort-state write is reported on stderr.
- `--stats` counts entries in a single pass (a map plus a slice) instead of
  rescanning the list for every entry.
- The `fzf --version` probe is cached for the process, and the leftover
  `is_frozen()` helper (always true in a compiled binary) is gone.

## [2.1] - 2026-10-09

### Changed
- The picker passes `--scheme=history` only when fzf is 0.33 or newer. An older
  fzf (Ubuntu 22.04 ships 0.29) used to make the picker fail outright; it now
  keeps working with the default scoring scheme, and `--doctor` shows the fzf
  version together with that note.
- Migration messages go to stderr, so `--json`, `--pick` and `--print-list`
  stay clean on a first run.
- `--self-test` grew to 53 offline checks (fzf version parsing and the 0.33
  minimum).
- CI: the Go module cache step is disabled - there are no dependencies, so
  there is no `go.sum` to cache.

## [2.0] - 2026-10-09

Second generation: the V1 idea (fzf over PowerShell history + cheat.sh + a saved
markdown file) rebuilt as a full workflow. This is the first public release:
before it, the whole 2.0 feature set was ported from a single Python script to
Go, so the shipped `histex.exe` needs no runtime.

### Added
- One data root: everything user-owned (config, `saved_recipes.md`, `scripts/`,
  `recipes.jsonl`, cache, `sort.state`, the prompt snippet) lives in
  `%APPDATA%\histex` on Windows (or `~/.histex` elsewhere). Override with
  `--data-dir DIR` or `HISTEX_DATA_DIR`; drop a `histex.portable` marker next
  to the exe for a portable install.
- Safe one-time migration from the old split locations (script dir +
  `~/.histex`): copy-only, never overwrites an existing target, never deletes
  an original, idempotent, and writes a `migrated.json` receipt. `--no-migrate`
  skips it.
- Explanation chain with first-hit-wins order: local cache -> PowerShell
  `Get-Help` -> local `tldr` pages -> cheat.sh. Works partly offline; secrets
  (`password`, `token`, `key`, ...) never leave the machine.
- `CTRL-T` save flow: title suggestion, tags, duplicate check, Markdown recipe,
  `recipes.jsonl` log, and runnable `.ps1` / `.bat` / `.cmd` / `.sh` export.
- `--browse`: the saved recipes as their own searchable library, with its own
  `recipes> ` prompt and a live preview of the stored commands.
- `--doctor`: one health check for the runtime, fzf (with its version), tldr,
  history, clipboard, recipes and the prompt snippet, with install hints.
- `--stats`, `--clean` (with automatic `.histex-backup`), `--today` / `--here`
  via an opt-in sidecar log, `--pick` / `--json` for scripting.
- Safety: secret guard before any network call, destructive-pattern warnings
  (`rm -rf`, `Remove-Item -Recurse`, `git reset --hard`, ...).
- `install.ps1`: one-shot Windows installer (checks the tools, builds and
  starts `histex.exe`, writes the config, runs doctor and self-test). It never
  touches `$PROFILE`.
- `--self-test`: offline checks (51 and counting).

### Changed
- **Rewritten in Go.** The Python single-file script is gone: histex is a Go
  program (standard library only) with the same CLI, the same data dir and
  byte-for-byte compatible output. No Python runtime is needed - the built
  `histex.exe` is self-contained, and fzf stays the only required tool.
- The version is stamped at build time (`-X main.version`), so a release always
  reports its own tag; the release workflow fails if the two ever differ.
- Config path fields (`recipes`, `scripts_dir`, `jsonl`, `cache_dir`) default
  to `null` and are resolved into the data dir at runtime; an explicit value
  still wins.
- All user data stays out of the program directory, so a packaged build never
  writes next to the exe.
- `--doctor` reports the Go runtime instead of Python, and the fzf
  reload/preview callbacks re-invoke the exe instead of `python histex.py`.
- The preview pane is hidden by default; `CTRL-P` (or `CTRL-/`) toggles it.
  `--show-preview` starts with it visible, `--no-preview` removes it entirely.
- Header is short and pinned on top:
  `ENTER explain | TAB mark | ^T save | ^O copy | ^P preview | ^R sort`.
- Picker prompts differ per mode: `histex> `, `recipes> `, `clean> `.
- Startup prints a recipes hint when the library is non-empty.
- `README.md`, `install.ps1` and the CI workflows build and run the Go binary
  (`go vet`, `go build`, `histex --self-test`); pushing a `v*` tag builds and
  publishes `histex.exe` with the GitHub Release.

### Removed
- `histex.py` and the Python tooling (`__pycache__`, the `py_compile` CI step).
- The V1 explainshell.com browser step. Explaining a PowerShell alias with a
  bash-only site gave wrong answers; local `Get-Help` + `tldr` replace it.

## [1.0] - 2026-10-03

- V1 (Python MVP): read `ConsoleHost_history.txt`, dedup, fzf picker,
  `cheat.sh` explanation on ENTER, `saved_recipes.md` on save.
