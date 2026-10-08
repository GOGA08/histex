# Changelog

All notable changes to histex. No browser integration - terminal and files only.

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
