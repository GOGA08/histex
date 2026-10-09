# Security policy

## Supported versions

Only the latest release is supported. histex ships as a self-contained binary,
so updating is a single download (see the README).

## Reporting a vulnerability

Please use GitHub's private reporting: open the **Security** tab of this
repository and click **Report a vulnerability**. That keeps the details private
until a fix exists. If that option is not available to you, open a normal issue
asking for a private channel - please do not paste exploit details into a public
issue.

A useful report says what you did, what happened, the version
(`histex --version`), your OS, and the `--doctor` output when it is relevant.

## Scope - what histex actually does

Worth knowing when judging a report:

- histex **never runs** the commands it shows. The explanation chain runs fixed
  helpers only: `Get-Help`, `tldr`, cheat.sh and - locally, as the last resort -
  `<tool> --help`. That one executes the tool's own binary with a hard-coded
  `--help`, never with the arguments you typed, with a 5 second timeout, closed
  stdin and the pagers disabled. Running a command stays the user's decision.
- Network access happens only for cheat.sh, and only for queries that pass the
  secret guard (`password`, `token`, `key`, ...). `--offline` disables it
  completely, and answers are cached under the data dir.
- It reads the shell history and writes recipes, scripts, cache and logs into
  `%APPDATA%\histex` (or the `--data-dir` given). `--clean` writes a
  `.histex-backup` file next to the history before changing anything, and
  `--restore` copies that backup back.
- `--install-snippets` writes a snippet file (PowerShell on Windows,
  `histex_profile.sh` for bash/zsh elsewhere) but never edits `$PROFILE`,
  `~/.bashrc` or `~/.zshrc`; adding the line is the user's decision.
- The binaries are **unsigned** by design - that is the free SmartScreen route
  described in the README, not a vulnerability by itself.
