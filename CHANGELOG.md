# Changelog

All notable changes to histex. No browser integration - terminal and files only.

## [Unreleased]

### Added
- One data root: everything user-owned (config, `saved_recipes.md`, `scripts/`,
  `recipes.jsonl`, cache, `sort.state`, the prompt snippet) now lives in
  `%APPDATA%\histex` on Windows (or `~/.histex` elsewhere). Override with
  `--data-dir DIR` or `HISTEX_DATA_DIR`; drop a `histex.portable` marker next
  to the script/exe for a portable install.
- Safe one-time migration from the old split locations (script dir +
  `~/.histex`): copy-only, never overwrites an existing target, never deletes
  an original, idempotent, and writes a `migrated.json` receipt. `--no-migrate`
  skips it.
- Frozen-exe awareness (`is_frozen()`), so a packaged build never writes next
  to the program directory.

### Changed
- Config path fields (`recipes`, `scripts_dir`, `jsonl`, `cache_dir`) default
  to `null` and are resolved into the data dir at runtime; an explicit value
  still wins.
- `--self-test` grew to 51 offline checks (migration, data paths, atomic
  writes, frozen self-command).
- **Rewritten in Go.** The Python single-file script is gone: histex is now a
  Go program (standard library only) with the same CLI, the same data dir and
  byte-for-byte compatible output. No Python runtime is needed - the built
  `histex.exe` is self-contained, and fzf stays the only required tool.
- `--doctor` now reports the Go runtime and the exe path, and the fzf
  reload/preview callbacks re-invoke the exe instead of `python histex.py`.
- `README.md`, `install.ps1` and the CI workflow build and run the Go binary
  (`go vet`, `go build`, `histex --self-test`) instead of the Python script.

### Removed
- `histex.py` and the Python tooling (`__pycache__`, the `py_compile` CI step).

## [2.0] - 2026-10-08

Second generation: the V1 idea (fzf over PowerShell history + cheat.sh + a
saved markdown file) rebuilt as a full workflow.

### Added
- Explanation chain with first-hit-wins order: local cache -> PowerShell
  `Get-Help` -> local `tldr` pages -> cheat.sh. Works partly offline; secrets
  (`password`, `token`, `key`, ...) never leave the machine.
- `CTRL-T` save flow: title suggestion, tags, duplicate check, Markdown recipe,
  `recipes.jsonl` log, and runnable `.ps1` / `.bat` / `.cmd` / `.sh` export.
- `--browse`: the saved recipes as their own searchable library, with its own
  `recipes> ` prompt and a live preview of the stored commands.
- `--doctor`: one health check for python, fzf, tldr, history, clipboard,
  recipes and the prompt snippet, with install hints.
- `--stats`, `--clean` (with automatic `.histex-backup`), `--today` / `--here`
  via an opt-in sidecar log, `--pick` / `--json` for scripting.
- Safety: secret guard before any network call, destructive-pattern warnings
  (`rm -rf`, `Remove-Item -Recurse`, `git reset --hard`, ...).
- `install.ps1`: one-shot Windows installer (checks tools, writes config,
  runs doctor + self-test). Never touches `$PROFILE`.
- `--self-test`: offline checks (37 and counting).

### Changed
- The preview pane is hidden by default; `CTRL-P` (or `CTRL-/`) toggles it.
  `--show-preview` starts with it visible, `--no-preview` removes it entirely.
- Header is short and pinned on top:
  `ENTER explain | TAB mark | ^T save | ^O copy | ^P preview | ^R sort`.
- Picker prompts differ per mode: `histex> `, `recipes> `, `clean> `.
- Startup prints a recipes hint when the library is non-empty.

### Removed
- The V1 explainshell.com browser step. Explaining a PowerShell alias with a
  bash-only site gave wrong answers; local `Get-Help` + `tldr` replace it.

## [1.0] - 2026-10-03

- V1 (Python MVP): read `ConsoleHost_history.txt`, dedup, fzf picker,
  `cheat.sh` explanation on ENTER, `saved_recipes.md` on save.
