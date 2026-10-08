# Changelog

All notable changes to histex. No browser integration - terminal and files only.

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
