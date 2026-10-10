# Contributing

Thanks for looking at histex. It is a small, single-binary Go program that
wraps [fzf](https://github.com/junegunn/fzf): one package, no third-party
dependencies.

## What you need

- [Go](https://go.dev/dl/) 1.27 or newer
- [fzf](https://github.com/junegunn/fzf) if you want the interactive picker
  while testing (`winget install junegunn.fzf`, `brew install fzf`, ...)
- optional: `tldr` / tealdeer, so explanations work offline

## Getting started

```powershell
git clone https://github.com/GOGA08/histex.git
cd histex
go build -o histex.exe .
.\histex.exe --doctor      # what is missing on this machine
.\histex.exe               # the picker
```

## Before you push

```powershell
gofmt -l .        # must print nothing: CI fails on unformatted files
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test ./...     # the same offline checks as --self-test
.\histex.exe --self-test
```

CI runs those steps on Windows and Ubuntu for every push and pull request, so a
green local run means a green build.

## How the code is laid out

| file | what lives there |
|---|---|
| `main.go` | the CLI flags and the order the modes are dispatched in |
| `config.go` | the data dir policy, config load/save, the one-time migration |
| `history.go` | history discovery, parsing, dedup, ordering, the local cache |
| `explain.go` | the explain chain, the query planner, both previews |
| `recipes.go` | Markdown recipes, the JSONL log, runnable scripts, `--browse` |
| `fzf.go` | the picker: flags, the NUL protocol, the sort toggle |
| `modes.go` | `--clean`, `--stats`, the sidecar log, `--pick` / `--json` |
| `snippet.go` | the PowerShell profile snippet |
| `doctor.go` | the `--doctor` rows |
| `selftest.go` | every offline check, shared by `--self-test` and `go test` |
| `textio.go` | the text I/O rules (UTF-8, BOM, CRLF) |
| `guards.go` | the secret and danger pattern guards |
| `platform_*.go` | the per-OS parts: clipboard, console, PowerShell |

## Adding a check

`--self-test` and `go test` share one list: add a `check("name", condition)`
line inside `runSelfChecks()` (`selftest.go`) and both pick it up. The number of
checks is mentioned in the CHANGELOG, so bump it there too if it changes.

## Conventions

- **standard library only** - please do not add modules; `go.mod` should stay
  free of `require` lines.
- gofmt formatting, tabs, no trailing whitespace (`.editorconfig` helps).
- comments explain *why*. Where the port from Python kept a quirk on purpose
  (CRLF handling, the byte-compatible recipes, the fallback layers), say so.
- human-facing output goes to **stderr** (`[i] ...`, `[!] ...`, `[x] ...`);
  stdout stays clean because it feeds fzf and `--json`.

## Releasing

1. move the `[Unreleased]` entries in `CHANGELOG.md` under a new version
   heading, and bump the dev-build `version` fallback in `config.go` (plus its
   `-ldflags` example) to that version, so a plain `go build` reports the
   latest release instead of a stale number
2. commit and push to `main`
3. tag and push the tag:

   ```powershell
   git tag vX.Y
   git push origin vX.Y
   ```

   The `release` workflow then builds five binaries (Windows, Linux amd64 and
   arm64, macOS arm64 and amd64), self-tests the native one, refuses to publish
   when `histex --version` does not match the tag, attaches `fzf.exe`,
   `fzf-LICENSE.txt` and `SHA256SUMS`, and creates the GitHub release.
4. bump `version` + `hash` in `packaging/scoop/histex.json`, and the version,
   URL and SHA256 in `packaging/winget/` - the steps are in
   `packaging/README.md`.

`TODO.md` is a local, git-ignored working list; it is intentionally not part of
the repository.
