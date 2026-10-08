#!/usr/bin/env python3
"""histex - interactive helper for shell command history (V2).

What it does:
  1) Reads command history (PowerShell 5.1 / PowerShell 7 / bash / zsh), joins
     broken multi-line entries, drops blank lines, duplicates and noise.
  2) Opens an fzf picker over the history (recent-first or frequency-first).
  3) Keys inside fzf:
        ENTER    explain the current / marked command(s)
        TAB      mark a command (SHIFT-TAB unmarks)
        CTRL-T   save the marked commands as a recipe (+ optional runnable script)
        CTRL-O   copy the marked / current commands to the clipboard
        CTRL-P   toggle the preview pane (hidden by default)
        CTRL-R   reload the list and toggle sorting (recent <-> frequent)

Explanations are looked up in this order (first hit wins, configurable through
the "explain_order" setting): local cache -> PowerShell Get-Help (cmdlets) ->
local tldr pages -> cheat.sh (network). Get-Help and tldr work offline.

Extra modes:
  --recipes   browse the saved recipes in fzf
  --clean     delete selected entries from the history file
  --stats     show usage statistics
  --pick      print only the chosen command(s)  (for shell integration)
  --json      machine-readable output (for scripting)
  --print-list / --preview / --self-test / --init-config

Dependencies: fzf (https://github.com/junegunn/fzf). Standard library only.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from urllib import error as urlerror
from urllib import parse as urlparse
from urllib import request as urlrequest

APP = "histex"
VERSION = "2.0"

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
HOME = os.path.expanduser("~")
CONFIG_PATH = os.path.join(HOME, ".histex", "config.json")

DEFAULT_CONFIG = {
    "recipes": os.path.join(SCRIPT_DIR, "saved_recipes.md"),
    "scripts_dir": os.path.join(SCRIPT_DIR, "scripts"),
    "jsonl": os.path.join(SCRIPT_DIR, "recipes.jsonl"),
    "history": None,               # None = auto-detect
    "shell": "auto",               # auto | ps5 | ps7 | bash | zsh
    "network": True,               # False = never touch the network
    "cache": True,
    "cache_ttl_hours": 168,
    "cache_dir": os.path.join(HOME, ".histex", "cache"),
    "sort": "recent",              # recent | freq
    "max_items": 5000,
    "detail": "short",             # short | full
    "explain_order": ["cache", "local", "tldr", "cheat"],
    "tldr": True,                  # use the local tldr pages when installed
    "preview_window": "right:40%:wrap",
    "exclude": [
        r"^(cls|clear|exit|quit)\s*$",
        r"^.{1,2}$",
        r"histex2?(\.py)?(\s|$)",
    ],
    "secrets": [
        r"password", r"passwd", r"token", r"secret", r"api[_-]?key",
        r"sshpass", r"bearer\s", r"private[_-]?key",
    ],
    "danger": [
        r"Remove-Item.*-(Recurse|Force)",
        r"\brm\s+-[a-z]*r[a-z]*f",
        r"git\s+reset\s+--hard",
        r"Stop-Process.*-Force",
        r"Format-Volume",
        r"DROP\s+(TABLE|DATABASE)",
    ],
    "expect": "ctrl-t,ctrl-x",
    "clip_key": "ctrl-o",
    "reload_key": "ctrl-r",
    "preview": False,            # hidden by default; CTRL-P toggles it in fzf
    "preview_key": "ctrl-p",
}


def load_config(path=None):
    """Merges ~/.histex/config.json over the built-in defaults."""
    config = dict(DEFAULT_CONFIG)
    path = path or os.environ.get("HISTEX_CONFIG") or CONFIG_PATH
    if os.path.isfile(path):
        try:
            with open(path, "r", encoding="utf-8-sig") as handle:
                user = json.load(handle)
            if isinstance(user, dict):
                for key, value in user.items():
                    if key in config:
                        config[key] = value
        except (OSError, ValueError) as exc:
            print("[!] ignoring %s: %s" % (path, exc), file=sys.stderr)
    config["_path"] = path
    return config


def write_default_config(path):
    """Creates a documented config file (used by --init-config)."""
    directory = os.path.dirname(path)
    if directory:
        os.makedirs(directory, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(DEFAULT_CONFIG, handle, indent=2, ensure_ascii=False)
        handle.write("\n")
    return path


def setup_console():
    """Force UTF-8 on stdout/stderr so command text is never mangled."""
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")
        except (AttributeError, ValueError):
            pass


# --- history sources (idea 24: multiple shells) -----------------------------
def history_candidates():
    """Returns [(label, path)] for every shell we know about."""
    appdata = os.environ.get("APPDATA")
    items = []
    if appdata:
        # Windows PowerShell 5.1
        items.append(("ps5", os.path.join(
            appdata, "Microsoft", "Windows", "PowerShell", "PSReadLine",
            "ConsoleHost_history.txt")))
        # PowerShell 7+
        items.append(("ps7", os.path.join(
            appdata, "Microsoft", "PowerShell", "PSReadLine",
            "ConsoleHost_history.txt")))
    # PowerShell 7+ on macOS / Linux
    items.append(("ps7", os.path.join(
        HOME, ".local", "share", "powershell", "PSReadLine",
        "ConsoleHost_history.txt")))
    items.append(("bash", os.path.join(HOME, ".bash_history")))
    items.append(("zsh", os.path.join(HOME, ".zsh_history")))
    return items


def resolve_history(config):
    """Returns (label, path) of the history file that will be used."""
    override = config.get("history")
    if override:
        return "custom", os.path.abspath(os.path.expanduser(override))
    wanted = config.get("shell") or "auto"
    for label, path in history_candidates():
        if wanted != "auto" and label != wanted:
            continue
        if os.path.isfile(path):
            return label, path
    return None, None


# --- reading + cleaning the history -----------------------------------------
ZSH_PREFIX = re.compile(r"^:\s*\d+:\d+;")


def _track_balance(line, state):
    """Updates (), [], {} and quote balance while scanning a single line."""
    closing = {")": "(", "]": "[", "}": "{"}
    index = 0
    length = len(line)
    while index < length:
        char = line[index]
        if state["quote"]:
            if char == "`":
                index += 2
                continue
            if char == state["quote"]:
                state["quote"] = None
            index += 1
            continue
        if char in ("'", '"'):
            state["quote"] = char
        elif char == "`":
            index += 2
            continue
        elif char in "([{":
            state["stack"].append(char)
        elif char in closing:
            if state["stack"] and state["stack"][-1] == closing[char]:
                state["stack"].pop()
        index += 1
    return state


def entries_from_text(text, join_continuations=True):
    """Splits history text into entries, rejoining multi-line commands.

    PSReadLine stores a multi-line command as several physical lines, so a line
    that leaves quotes or brackets open (or ends with a backtick) is glued to
    the following line(s).
    """
    lines = []
    for line in text.splitlines():
        # bash with HISTTIMEFORMAT writes "#<epoch>" marker lines
        if len(line) == 11 and line.startswith("#") and line[1:].isdigit():
            continue
        lines.append(ZSH_PREFIX.sub("", line))     # zsh: ": 1699999999:0;cmd"

    if not join_continuations:
        return [line for line in lines if line.strip()]

    entries = []
    buffer = []
    state = None
    for line in lines:
        if not buffer:
            if not line.strip():
                continue
            buffer = [line]
            state = {"quote": None, "stack": []}
        else:
            buffer.append(line)
        _track_balance(line, state)
        if not state["quote"] and not state["stack"] and not line.rstrip().endswith("`"):
            entries.append("\n".join(buffer))
            buffer = []
    if buffer:
        entries.append("\n".join(buffer))
    return [entry for entry in entries if entry.strip()]


def normalize_entry(entry):
    """Collapses whitespace so that near-duplicates compare equal."""
    return re.sub(r"\s+", " ", entry).strip()


def dedup_entries(entries):
    """Removes duplicates by normalized form; the newest variant comes first."""
    seen = set()
    result = []
    for entry in reversed(entries):
        key = normalize_entry(entry)
        if key in seen:
            continue
        seen.add(key)
        result.append(entry)
    return result


def is_noise(entry, patterns):
    stripped = entry.strip()
    if not stripped:
        return True
    for pattern in patterns:
        try:
            if re.search(pattern, stripped, re.IGNORECASE):
                return True
        except re.error:
            continue
    return False


def count_entries(entries):
    counts = {}
    for entry in entries:
        key = normalize_entry(entry)
        counts[key] = counts.get(key, 0) + 1
    return counts


def order_entries(entries, sort_mode, count_source=None):
    """Entries arrive newest-first; 'freq' reorders them by usage count.

    count_source must be the *raw* (pre-deduplication) list, otherwise every
    single count would be 1 and frequency sorting would do nothing.
    """
    if sort_mode != "freq":
        return entries
    counts = count_entries(entries if count_source is None else count_source)
    return sorted(entries, key=lambda item: -counts.get(normalize_entry(item), 1))


def raw_entries(config, join_continuations=True):
    """All history entries, newest first, without deduplication."""
    label, path = resolve_history(config)
    if not path or not os.path.isfile(path):
        return label, path, []
    with open(path, "r", encoding="utf-8-sig", errors="replace") as handle:
        text = handle.read()
    entries = entries_from_text(text, join_continuations)
    return label, path, [e for e in entries if not is_noise(e, config["exclude"])]


def load_history(config, join_continuations=True):
    """Returns (label, path, entries) deduplicated and ordered."""
    label, path, raw = raw_entries(config, join_continuations)
    entries = dedup_entries(raw)
    entries = order_entries(entries, config.get("sort", "recent"), count_source=raw)
    limit = int(config.get("max_items") or 0)
    if limit > 0:
        entries = entries[:limit]
    return label, path, entries


# --- local cache (idea 5) ---------------------------------------------------
def cache_path(config, key):
    safe = re.sub(r"[^A-Za-z0-9._-]+", "_", key)[:80].strip("_") or "entry"
    return os.path.join(config["cache_dir"], safe + ".txt")


def cache_get(config, key):
    if not config.get("cache"):
        return None
    path = cache_path(config, key)
    if not os.path.isfile(path):
        return None
    try:
        ttl = float(config.get("cache_ttl_hours") or 0) * 3600
        if ttl and (time.time() - os.path.getmtime(path)) > ttl:
            return None
        with open(path, "r", encoding="utf-8") as handle:
            return handle.read()
    except (OSError, ValueError):
        return None


def cache_put(config, key, body):
    if not config.get("cache") or not body:
        return
    try:
        os.makedirs(config["cache_dir"], exist_ok=True)
        with open(cache_path(config, key), "w", encoding="utf-8") as handle:
            handle.write(body)
    except OSError:
        pass


# --- safety guards (ideas 6 and 7) ------------------------------------------
def matches_any(text, patterns):
    for pattern in patterns or []:
        try:
            if re.search(pattern, text, re.IGNORECASE):
                return pattern
        except re.error:
            continue
    return None


def is_secret(entry, config):
    """Returns the matched pattern if the command looks like it holds a secret."""
    return matches_any(entry, config.get("secrets"))


def danger_reason(entry, config):
    """Returns the matched pattern if the command looks destructive."""
    return matches_any(entry, config.get("danger"))


# --- PowerShell helpers -----------------------------------------------------
def run_powershell(script, timeout=20):
    """Runs a PowerShell snippet and returns its stdout ('' when unavailable)."""
    if os.name != "nt":
        return ""
    exe = shutil.which("powershell") or shutil.which("pwsh")
    if not exe:
        return ""
    try:
        proc = subprocess.run(
            [exe, "-NoProfile", "-NonInteractive", "-Command", script],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
        )
        return proc.stdout or ""
    except (OSError, subprocess.SubprocessError):
        return ""


def _normalize_token(token):
    """Cleans up a command name: quotes, call operator, path."""
    clean = token.strip().strip("\"'").lstrip("&")
    if "/" in clean or "\\" in clean:
        clean = os.path.basename(clean.replace("\\", "/"))
    return clean


_ALIAS_CACHE = {}
_NAME_OK = re.compile(r"^[A-Za-z][A-Za-z0-9_.-]*$")


def resolve_alias(name, config):
    """Maps a PowerShell alias to its definition (ls -> Get-ChildItem)."""
    if not name or os.name != "nt" or not _NAME_OK.match(name):
        return None
    key = name.lower()
    if key in _ALIAS_CACHE:
        return _ALIAS_CACHE[key]
    out = run_powershell(
        "$a = Get-Alias -Name '%s' -ErrorAction SilentlyContinue; "
        "if ($a) { $a.Definition }" % name)
    resolved = out.strip().splitlines()[0].strip() if out.strip() else None
    _ALIAS_CACHE[key] = resolved
    return resolved


def alias_warning(name, definition):
    """Warns about aliases that shadow a well-known Unix tool (idea 4)."""
    if definition and name.lower() in ("curl", "wget") and definition.lower() != name.lower():
        return ("'%s' in PowerShell is an alias for %s, not the real %s tool "
                "- use %s.exe for the real one." % (name, definition, name, name))
    return None


def ps_command_type(name):
    """Returns Cmdlet / Function / Application / ... for a name (Windows only)."""
    if os.name != "nt" or not _NAME_OK.match(name or ""):
        return None
    out = run_powershell(
        "$c = Get-Command -Name '%s' -ErrorAction SilentlyContinue; "
        "if ($c) { $c.CommandType.ToString() }" % name)
    return out.strip().splitlines()[0].strip() if out.strip() else None


def local_explain(command, config):
    """Local PowerShell help for cmdlets - works offline (idea 3).

    Returns (text, is_rich). is_rich=False means the help database is thin
    (Update-Help was never run), so cheat.sh should be preferred.
    """
    tokens = command.split()
    if not tokens:
        return None, False
    name = _normalize_token(tokens[0])
    kind = ps_command_type(name)
    if kind not in ("Cmdlet", "Function", "Filter", "Configuration", "Script",
                    "ExternalScript"):
        return None, False
    switch = "-Full" if config.get("detail") == "full" else "-Examples"
    out = run_powershell(
        "(Get-Help -Name '%s' %s -ErrorAction SilentlyContinue | "
        "Out-String -Width 200)" % (name, switch), timeout=25)
    text = (out or "").strip()
    if len(text) < 40:
        return None, False
    rich = "EXAMPLE" in text.upper() and len(text) > 400
    return text, rich


# --- online lookup: cheat.sh ------------------------------------------------
CHEAT_URL = "https://cheat.sh/"
CHEAT_UA = "curl/8.5.0"       # mandatory: otherwise cheat.sh answers with HTML
CHEAT_TIMEOUT = 10


def cheat_miss(body):
    """cheat.sh answers HTTP 200 even for a miss, so the body must be checked."""
    lowered = body.lower()
    return ("404 not found" in lowered
            or "unknown topic" in lowered
            or "unknown cheat sheet" in lowered)


def fetch_cheat(query):
    """Returns the cheat sheet text, or None when nothing was found."""
    url = CHEAT_URL + urlparse.quote(query, safe="") + "?T"
    request = urlrequest.Request(url, headers={"User-Agent": CHEAT_UA})
    with urlrequest.urlopen(request, timeout=CHEAT_TIMEOUT) as response:
        body = response.read().decode("utf-8", errors="replace")
    return None if cheat_miss(body) else body


# --- query planning ---------------------------------------------------------
def split_pipeline(command):
    """Splits 'a | b; c && d' into single commands (quote aware, idea 14)."""
    parts = []
    buffer = []
    quote = None
    index = 0
    while index < len(command):
        char = command[index]
        if quote:
            buffer.append(char)
            if char == quote:
                quote = None
            index += 1
            continue
        if char in ("'", '"'):
            quote = char
            buffer.append(char)
            index += 1
            continue
        pair = command[index:index + 2]
        if pair in ("&&", "||"):
            parts.append("".join(buffer))
            buffer = []
            index += 2
            continue
        if char in "|;":
            parts.append("".join(buffer))
            buffer = []
            index += 1
            continue
        buffer.append(char)
        index += 1
    parts.append("".join(buffer))
    return [part.strip() for part in parts if part.strip()]


def flags_in(command):
    """Extracts -x / --long flags found in the command (idea 16, lightweight)."""
    return sorted(set(re.findall(r"(?<!\S)--?[A-Za-z][A-Za-z0-9-]*", command)))


def candidates(command, config, resolve=True):
    """Ordered query candidates for an external lookup.

    'git status -sb' -> 'git status' -> 'git'. PowerShell aliases are resolved
    as well, so 'ls' also tries 'get-childitem' (idea 4). Previews pass
    resolve=False because spawning PowerShell on every keystroke is too slow.
    """
    tokens = command.split()
    if not tokens:
        return []
    first = _normalize_token(tokens[0])
    raw = []
    if len(tokens) >= 2 and not tokens[1].startswith("-"):
        raw.append(first + " " + _normalize_token(tokens[1]))
    raw.append(first)
    alias = resolve_alias(first, config) if resolve else None
    if alias:
        raw.append(_normalize_token(alias))
    queries = []
    for item in raw:
        item = item.lower()
        if item and item not in queries:
            queries.append(item)
    return queries


def _explain_tail(command, config):
    """Extra detail in 'full' mode: the flags used by the command (idea 15/16)."""
    if config.get("detail") != "full":
        return
    flags = flags_in(command)
    if flags:
        print("")
        print("flags used: %s" % " ".join(flags))


# --- local tldr pages (offline source, idea 16) -----------------------------
def tldr_path():
    return shutil.which("tldr")


def _run_tldr(exe, query):
    """Runs the tldr client for one query. Returns (text, hint)."""
    hint = None
    for args in (["-q", query], ["-q", "--platform", "windows", query]):
        try:
            proc = subprocess.run(
                [exe] + args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, encoding="utf-8", errors="replace", timeout=20)
        except (OSError, subprocess.SubprocessError):
            return None, None
        out = (proc.stdout or "").strip()
        error = proc.stderr or ""
        if "cache not found" in error.lower():
            hint = "tldr page cache is missing - run:  tldr --update"
            continue
        if len(out) > 20:
            return out, None
    return None, hint


def tldr_lookup(command, config):
    """Local tldr pages - they work offline once `tldr --update` has run.

    The result is mirrored into histex's own cache, so --preview and --offline
    can use it later without starting tldr again.
    """
    if not config.get("tldr", True):
        return None
    exe = tldr_path()
    if not exe:
        return None
    hint = None
    for query in candidates(command, config, resolve=False):
        text, hint = _run_tldr(exe, query)
        if text:
            cache_put(config, "tldr:" + query, text)
            return text
    if hint:
        print("[!] " + hint, file=sys.stderr)
    return None


def update_tldr():
    """--update-tldr: refresh the local tldr page cache (tldr --update)."""
    exe = tldr_path()
    if not exe:
        print("[x] tldr is not installed.", file=sys.stderr)
        print("    Install it with:", file=sys.stderr)
        if os.name == "nt":
            print("    winget install dbrgn.tealdeer", file=sys.stderr)
        elif sys.platform == "darwin":
            print("    brew install tealdeer", file=sys.stderr)
        else:
            print("    cargo install tealdeer   (or: sudo apt install tealdeer)",
                  file=sys.stderr)
        return 1
    print("[..] tldr --update", file=sys.stderr, flush=True)
    try:
        proc = subprocess.run([exe, "--update"])
    except (OSError, subprocess.SubprocessError) as exc:
        print("[x] could not run tldr: %s" % exc, file=sys.stderr)
        return 1
    if proc.returncode != 0:
        print("[x] tldr --update failed (exit code %s)" % proc.returncode,
              file=sys.stderr)
        return 1
    print("[ok] local tldr page cache updated.")
    return 0


def cheat_lookup(queries, config):
    """cheat.sh lookup; the cache is checked per query, before each request."""
    for index, query in enumerate(queries):
        cached = cache_get(config, query)
        if cached:
            return cached
        print("[..] cheat.sh: %s" % query, file=sys.stderr, flush=True)
        try:
            body = fetch_cheat(query)
        except urlerror.HTTPError as exc:
            if exc.code == 429:
                print("[!] cheat.sh: 429 (rate limited) - try again later; cached "
                      "answers still work offline.", file=sys.stderr)
            else:
                print("[!] cheat.sh: HTTP %s" % exc.code, file=sys.stderr)
            return None
        except urlerror.URLError as exc:
            print("[!] network error: %s (offline?)" % (exc.reason,), file=sys.stderr)
            return None
        except OSError as exc:
            print("[!] network error: %s" % exc, file=sys.stderr)
            return None
        if body:
            cache_put(config, query, body)
            return body
        if index + 1 < len(queries):
            print("     no entry for '%s' - falling back to '%s'"
                  % (query, queries[index + 1]), file=sys.stderr)
    return None


def preview_text(command, config):
    """Text for the fzf preview pane: the command + a cached answer if any.

    Long commands are truncated in the list, so the first block here is the
    full command - that is what makes the preview useful right away. It must
    stay instant: nothing here starts tldr, PowerShell or a network request.
    """
    segments = [segment.strip() for segment in split_pipeline(command)]
    head = "\n\n".join(segments)
    if len(segments) > 1:
        head += "\n\n(%d commands joined by | ; && ||)" % len(segments)

    for query in candidates(command, config, resolve=False):
        for key in ("tldr:" + query, query):
            cached = cache_get(config, key)
            if cached:
                return "%s\n\n%s\n\n%s" % (head, "-" * 24, cached.strip())
    return "%s\n\n%s\n\nno cached explanation yet - press ENTER to explain" % (
        head, "-" * 24)


def explain(command, config, preview=False):
    """Prints an explanation for one command. Returns the source label."""
    if preview:
        print(preview_text(command, config))
        return "preview"

    segments = split_pipeline(command)
    if len(segments) > 1:
        for position, segment in enumerate(segments, 1):
            print("")
            print("--- [%d/%d] %s" % (position, len(segments), segment))
            explain(segment, config, preview=preview)
        return "pipeline"

    tokens = command.split()
    if not tokens:
        return "none"

    queries = candidates(command, config)
    warning = alias_warning(tokens[0], resolve_alias(_normalize_token(tokens[0]), config))
    if warning:
        print("[!] " + warning, file=sys.stderr)

    allow_network = bool(config.get("network", True))
    secret = is_secret(command, config)
    if secret:
        print("[!] looks like a secret ('%s') - skipping the network." % secret,
              file=sys.stderr)
        allow_network = False

    # Local PowerShell help is fetched once: rich help is a source of its own,
    # thin help (Update-Help never run) is only the very last resort.
    local_text, rich = local_explain(command, config)

    order = config.get("explain_order") or ["cache", "local", "tldr", "cheat"]
    for source in order:
        text = None
        if source == "cache":
            text = cache_get(config, queries[0]) if queries else None
        elif source == "local":
            text = local_text if rich else None
        elif source == "tldr":
            text = tldr_lookup(command, config)
        elif source == "cheat":
            text = cheat_lookup(queries, config) if allow_network else None
        if text:
            print("")
            print(text.strip())
            _explain_tail(command, config)
            return source

    # Thin PowerShell help, if that is all we managed to find.
    if local_text:
        print("")
        print(local_text)
        _explain_tail(command, config)
        return "local(thin)"

    if not allow_network:
        print("[i] nothing local for: %s (run once without --offline, and make sure "
              "`tldr --update` has run)" % command, file=sys.stderr)
    else:
        print("[i] nothing found for: %s" % command, file=sys.stderr)
    return "none"


# --- clipboard (idea 1) -----------------------------------------------------
def _run_pipe(command, text):
    try:
        proc = subprocess.run(
            command, input=text, text=True, encoding="utf-8", errors="replace",
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return proc.returncode == 0
    except OSError:
        return False


def _clip_unicode_windows(text):
    exe = shutil.which("powershell") or shutil.which("pwsh")
    if not exe:
        return False
    try:
        proc = subprocess.run(
            [exe, "-NoProfile", "-NonInteractive", "-Command",
             "Set-Clipboard -Value $args[0]", text],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)
        return proc.returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False


def clipboard_copy(text):
    """Copies text to the system clipboard. ASCII is fast, Unicode is safe."""
    if not text:
        return False
    if os.name == "nt":
        if text.isascii():
            return _run_pipe(["clip"], text)
        return _clip_unicode_windows(text)
    if sys.platform == "darwin":
        return _run_pipe(["pbcopy"], text)
    for command in (["xclip", "-selection", "clipboard"], ["wl-copy"]):
        if shutil.which(command[0]) and _run_pipe(command, text):
            return True
    return False


# --- recipes: markdown, JSONL, runnable scripts (ideas 18-22) --------------
SCRIPT_EXTENSIONS = {"2": ".ps1", "3": ".bat", "4": ".sh", "5": ".cmd"}


def slugify(text, fallback="recipe"):
    slug = re.sub(r"[^A-Za-z0-9._-]+", "-", text or "").strip("-._").lower()
    return slug[:60] or fallback


def script_body(commands, extension):
    joined = "\n".join(commands)
    if extension in (".bat", ".cmd"):
        return "@echo off\r\n" + joined.replace("\n", "\r\n") + "\r\n"
    if extension == ".sh":
        return "#!/usr/bin/env bash\nset -euo pipefail\n\n" + joined + "\n"
    if extension == ".ps1":
        return ("#Requires -Version 5.1\n"
                "$ErrorActionPreference = 'Stop'\n\n" + joined + "\n")
    return joined + "\n"


def write_script(config, title, commands, extension):
    """Writes a runnable script for the selected commands (idea 21 + request)."""
    directory = config.get("scripts_dir") or os.path.join(SCRIPT_DIR, "scripts")
    os.makedirs(directory, exist_ok=True)
    path = os.path.join(directory, slugify(title) + extension)
    # cmd.exe chokes on a UTF-8 BOM; PowerShell needs one for non-ASCII text.
    encoding = "utf-8" if extension in (".bat", ".cmd", ".sh") else "utf-8-sig"
    with open(path, "w", encoding=encoding, newline="") as handle:
        handle.write(script_body(commands, extension))
    return path


def recipes_append(config, title, commands, tags=None):
    """Appends a recipe block to the markdown file (ideas 18/21)."""
    path = config.get("recipes") or DEFAULT_CONFIG["recipes"]
    directory = os.path.dirname(path)
    if directory:
        os.makedirs(directory, exist_ok=True)
    stamp = time.strftime("%Y-%m-%d %H:%M")
    heading = "### Date: %s - %s" % (stamp, title)
    if tags:
        heading += "   <!-- tags: %s -->" % ", ".join(tags)
    block = "%s\n\n```bash\n%s\n```\n\n" % (heading, "\n".join(commands))
    # A UTF-8 BOM stops PowerShell 5.1's Get-Content from garbling non-ASCII.
    encoding = "utf-8" if os.path.exists(path) else "utf-8-sig"
    with open(path, "a", encoding=encoding) as handle:
        handle.write(block)
    return path


def jsonl_append(config, title, commands, tags, scripts):
    """Mirrors every recipe in machine-readable form (idea 22)."""
    path = config.get("jsonl")
    if not path:
        return None
    try:
        directory = os.path.dirname(path)
        if directory:
            os.makedirs(directory, exist_ok=True)
        record = {
            "ts": time.strftime("%Y-%m-%d %H:%M:%S"),
            "title": title,
            "tags": tags or [],
            "commands": commands,
            "scripts": scripts or [],
        }
        with open(path, "a", encoding="utf-8") as handle:
            handle.write(json.dumps(record, ensure_ascii=False) + "\n")
        return path
    except OSError:
        return None


def recipe_exists(config, commands):
    """True when this exact command set is already stored (idea 19)."""
    path = config.get("recipes") or DEFAULT_CONFIG["recipes"]
    if not os.path.isfile(path):
        return False
    try:
        with open(path, "r", encoding="utf-8-sig", errors="replace") as handle:
            text = handle.read()
    except OSError:
        return False
    return "\n".join(commands).strip() in text


def ask(prompt, default=""):
    """input() that survives EOF/CTRL-C. Returns default, or None if cancelled."""
    try:
        answer = input(prompt).strip()
    except (EOFError, KeyboardInterrupt):
        print("")
        return None
    return answer or default


def ask_title(commands):
    """Asks for a title, suggesting one derived from the command (idea 20)."""
    first = commands[0].split()[0] if commands and commands[0].split() else "recipe"
    suggestion = _normalize_token(first) or "recipe"
    return ask("Title [%s]: " % suggestion, suggestion)


def save_recipe_flow(commands, config):
    """CTRL-T action: title, tags, markdown + optional runnable script."""
    print("")
    print("Marked commands (%d):" % len(commands))
    for command in commands:
        print("   " + command.splitlines()[0] + (" ..." if "\n" in command else ""))
    print("")
    for command in commands:
        reason = danger_reason(command, config)
        if reason:
            print("[!] destructive pattern '%s' in: %s"
                  % (reason, command.splitlines()[0]), file=sys.stderr)
    if recipe_exists(config, commands):
        answer = ask("This exact command set is already saved. Add it again? [y/N]: ")
        if answer is None or answer.lower() not in ("y", "yes"):
            print("[i] nothing saved.")
            return False
    title = ask_title(commands)
    if title is None:
        print("[i] cancelled.")
        return False
    tags = [tag.strip() for tag in
            (ask("Tags (comma separated, optional): ") or "").split(",") if tag.strip()]
    print("Format:  [1] markdown   [2] +PowerShell .ps1   [3] +batch .bat   "
          "[4] +bash .sh   (combine, e.g. 1,2)")
    choice = ask("Choice [1]: ", "1") or "1"
    scripts = []
    for key in re.split(r"[\s,]+", choice):
        extension = SCRIPT_EXTENSIONS.get(key)
        if not extension:
            continue
        if extension in (".bat", ".cmd") and not "\n".join(commands).isascii():
            print("[!] batch files do not handle non-ASCII text well - "
                  "consider .ps1 instead.", file=sys.stderr)
        try:
            scripts.append(write_script(config, title, commands, extension))
        except OSError as exc:
            print("[x] could not write script: %s" % exc, file=sys.stderr)
    for path in scripts:
        print("[ok] script: %s" % path)
    try:
        path = recipes_append(config, title, commands, tags)
    except OSError as exc:
        print("[x] could not write the recipe file: %s" % exc, file=sys.stderr)
        return False
    jsonl_append(config, title, commands, tags, scripts)
    print("[ok] recipe: %s" % path)
    return True


# --- fzf --------------------------------------------------------------------
FZF_HEADER = ("ENTER explain | TAB mark | ^T save | ^O copy | ^P preview | ^R sort")
SORT_STATE = os.path.join(os.path.dirname(CONFIG_PATH), "sort.state")


def fzf_path():
    return shutil.which("fzf")


def fzf_install_hint():
    """Installation instructions for the current OS."""
    if os.name == "nt":
        return ("    winget install junegunn.fzf     "
                "(alternatives: scoop install fzf | choco install fzf)")
    if sys.platform == "darwin":
        return "    brew install fzf"
    return "    sudo apt install fzf     (or: sudo dnf install fzf | pacman -S fzf)"


def _quoted(value):
    return '"%s"' % str(value).replace('"', "")


def self_command(*extra):
    """Command line that re-invokes this script (used by reload / preview)."""
    parts = [_quoted(sys.executable), _quoted(os.path.abspath(__file__))]
    return " ".join(parts + list(extra))


def list_text(entries):
    """NUL-separated list, so that multi-line commands survive as one item."""
    return "\0".join(entries) + "\0"


def toggle_sort(config):
    """Flips recent <-> freq and remembers it for the next reload (idea 8/13)."""
    current = config.get("sort", "recent")
    try:
        if os.path.isfile(SORT_STATE):
            with open(SORT_STATE, "r", encoding="utf-8") as handle:
                current = handle.read().strip() or current
    except OSError:
        pass
    new_mode = "freq" if current != "freq" else "recent"
    try:
        os.makedirs(os.path.dirname(SORT_STATE), exist_ok=True)
        with open(SORT_STATE, "w", encoding="utf-8") as handle:
            handle.write(new_mode)
    except OSError:
        pass
    return new_mode


def print_list(config, do_toggle=False):
    """Writes the ready-to-use list to stdout (used by fzf reload)."""
    if do_toggle:
        config = dict(config)
        config["sort"] = toggle_sort(config)
    _, _, entries = load_history(config)
    sys.stdout.write(list_text(entries))
    return entries


def do_clipboard(selected):
    """CTRL-O action: copy the marked / current command(s) (idea 1)."""
    text = "\n".join(selected)
    if clipboard_copy(text):
        print("[ok] copied %d command(s) to the clipboard." % len(selected))
        return True
    print("[x] could not reach the clipboard.", file=sys.stderr)
    return False


def run_fzf(entries, config, allow_preview=True):
    """Opens the picker. Returns (status, key, selected)."""
    exe = fzf_path()
    if not exe:
        print("[x] fzf not found in PATH - histex requires fzf.", file=sys.stderr)
        print("    Install it with:", file=sys.stderr)
        print(fzf_install_hint(), file=sys.stderr)
        print("    Then open a new terminal so PATH is refreshed.", file=sys.stderr)
        return "error", None, []

    expect = [key.strip() for key in (config.get("expect") or "").split(",") if key.strip()]
    for extra in ("ctrl-o",):
        if extra not in expect:
            expect.append(extra)

    args = [
        exe,
        "--multi",
        "--expect=" + ",".join(expect),
        "--read0",
        "--print0",
        "--scheme=history",
        "--height=80%",
        "--border",
        "--layout=reverse",
        "--marker=> ",
        "--pointer=>",
        "--prompt=histex> ",
        "--header=" + FZF_HEADER,
        "--header-first",
        "--bind=" + (config.get("reload_key") or "ctrl-r")
        + ":reload(" + self_command("--print-list", "--toggle-sort") + ")",
    ]
    if allow_preview and not config.get("_preview_off"):
        window = str(config.get("preview_window") or "right:40%:wrap")
        if not config.get("preview") and "hidden" not in window:
            window += ",hidden"      # hidden by default; toggle it with CTRL-P
        args.append("--preview=" + self_command("--preview", "{}"))
        args.append("--preview-window=" + window)
        preview_key = (config.get("preview_key") or "ctrl-p").strip() or "ctrl-p"
        args.append("--bind=" + preview_key + ":toggle-preview")
        if preview_key != "ctrl-/":
            args.append("--bind=ctrl-/:toggle-preview")

    try:
        proc = subprocess.run(
            args,
            input=list_text(entries),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except OSError as exc:
        print("[x] could not start fzf: %s" % exc, file=sys.stderr)
        return "error", None, []

    if proc.returncode in (1, 130):        # 1 = no match, 130 = ESC / CTRL-C
        return "cancel", None, []
    if proc.returncode != 0:
        print("[x] fzf exited with code %s" % proc.returncode, file=sys.stderr)
        if proc.stderr.strip():
            print(proc.stderr.strip(), file=sys.stderr)
        return "error", None, []

    # --print0: every field is NUL terminated. With --expect the very first
    # field is the pressed key, and that field is EMPTY for ENTER - so empty
    # fields must not be filtered out before the key has been read.
    fields = proc.stdout.split("\0")
    if fields and fields[-1] == "":
        fields.pop()
    key = ""
    if fields and fields[0] == "":
        fields = fields[1:]                  # explicit empty key field = ENTER
    elif fields and fields[0] in expect:
        key = fields[0]
        fields = fields[1:]
    return "ok", key, fields


# --- extra modes ------------------------------------------------------------
def parse_recipes(config, text=None):
    """Reads saved markdown recipes into [(title, tags, commands)] (idea 17)."""
    if text is None:
        path = config.get("recipes") or DEFAULT_CONFIG["recipes"]
        if not os.path.isfile(path):
            return []
        try:
            with open(path, "r", encoding="utf-8-sig", errors="replace") as handle:
                text = handle.read()
        except OSError:
            return []
    recipes = []
    title = None
    tags = []
    commands = []
    in_code = False
    for line in text.splitlines():
        if line.startswith("### "):
            if title is not None:
                recipes.append((title, tags, commands))
            heading = line[4:].strip()
            tags = []
            marker = re.search(r"<!--\s*tags:\s*([^>]*?)-->", heading)
            if marker:
                tags = [tag.strip() for tag in marker.group(1).split(",") if tag.strip()]
                heading = heading[:marker.start()].strip()
            if heading.lower().startswith("date: "):
                heading = heading[6:].strip()
            # Drop the timestamp so the browser shows only the real title.
            heading = re.sub(r"^\d{4}-\d{2}-\d{2}(\s+\d{2}:\d{2})?\s*-\s*", "",
                             heading).strip()
            title, commands, in_code = heading, [], False
            continue
        if title is None:
            continue
        if line.startswith("```"):
            in_code = not in_code
            continue
        if in_code:
            commands.append(line)
    if title is not None:
        recipes.append((title, tags, commands))
    return recipes


def recipes_mode(config):
    """--recipes: search the saved recipes, print and copy one (idea 17)."""
    recipes = parse_recipes(config)
    if not recipes:
        print("[i] no recipes yet - save one with CTRL-T first.", file=sys.stderr)
        return 0
    lookup = {}
    display = []
    for index, (title, tags, commands) in enumerate(recipes):
        label = title + (("   [%s]" % ", ".join(tags)) if tags else "")
        line = "%s   (%d cmd)" % (label, len(commands))
        while line in lookup:
            line += " "
        lookup[line] = index
        display.append(line)

    status, _, selected = run_fzf(display, config, allow_preview=False)
    if status != "ok" or not selected:
        return 0
    index = lookup.get(selected[0])
    if index is None:
        return 0
    title, tags, commands = recipes[index]
    print("")
    print("### %s%s" % (title, ("   [%s]" % ", ".join(tags)) if tags else ""))
    for command in commands:
        print("    " + command)
    print("")
    do_clipboard(commands)
    return 0


def clean_mode(config):
    """--clean: delete selected entries from the history file (idea 27)."""
    label, path, entries = load_history(config, join_continuations=False)
    if not path:
        print("[x] no history file found.", file=sys.stderr)
        return 1
    status, _, selected = run_fzf(entries, config, allow_preview=False)
    if status != "ok" or not selected:
        return 0
    doomed = set(selected)
    try:
        with open(path, "rb") as handle:
            raw = handle.read()
    except OSError as exc:
        print("[x] could not read the history file: %s" % exc, file=sys.stderr)
        return 1
    had_bom = raw[:3] == b"\xef\xbb\xbf"
    lines = raw.decode("utf-8-sig", errors="replace").splitlines()
    kept = [line for line in lines if line not in doomed]
    removed = len(lines) - len(kept)
    if not removed:
        print("[i] nothing matched - the file was left untouched.")
        return 0
    backup = path + ".histex-backup"
    try:
        shutil.copyfile(path, backup)
        encoding = "utf-8-sig" if had_bom else "utf-8"
        with open(path, "w", encoding=encoding, newline="") as handle:
            handle.write("\n".join(kept) + "\n")
    except OSError as exc:
        print("[x] could not rewrite the history file: %s" % exc, file=sys.stderr)
        return 1
    print("[ok] removed %d line(s); backup kept at %s" % (removed, backup))
    return 0


def stats_mode(config):
    """--stats: usage statistics (idea 26)."""
    label, path, raw = raw_entries(config)
    if not path:
        print("[x] no history file found.", file=sys.stderr)
        return 1
    unique = dedup_entries(raw)
    print("history file : %s   (%s)" % (path, label))
    print("entries      : %d total, %d unique" % (len(raw), len(unique)))
    counts = count_entries(raw)
    print("")
    print("top 15 commands:")
    for entry, count in sorted(counts.items(), key=lambda pair: -pair[1])[:15]:
        print("   %4d x  %s" % (count, entry.splitlines()[0][:70]))
    tools = {}
    for entry in raw:
        tokens = entry.split()
        name = _normalize_token(tokens[0]).lower() if tokens else ""
        if name:
            tools[name] = tools.get(name, 0) + 1
    print("")
    print("top 15 tools:")
    for name, count in sorted(tools.items(), key=lambda pair: -pair[1])[:15]:
        print("   %4d x  %s" % (count, name))
    if len(unique) != len(raw):
        print("")
        print("noise removed from the picker: %d entries" % (len(raw) - len(unique)))
    print("")
    print("explain order : %s" % " -> ".join(config.get("explain_order") or []))
    print("tldr source   : %s" % (tldr_path() or "not installed"))
    return 0


# --- shell integration (ideas 2 and 23) -------------------------------------
def profile_snippet():
    """PowerShell snippet: histex <-> prompt integration + sidecar log."""
    log_file = os.path.join(SCRIPT_DIR, "history_log.tsv")
    python = _quoted(sys.executable)
    script = os.path.abspath(__file__)
    lines = [
        "# --- histex integration " + "-" * 52,
        '# 1) Type "histex", pick a command, and it lands on your prompt:',
        "function histex {",
        "    param([Parameter(ValueFromRemainingArguments = $true)]$Rest)",
        '    $picked = & ' + python + ' "' + script + '" --pick @Rest',
        "    if ($picked) { [Microsoft.PowerShell.PSConsoleReadLine]::Insert($picked) }",
        "}",
        "",
        "# 2) Sidecar log (timestamp + directory). The plain PSReadLine history",
        "#    has neither, so this is what enables --today and --here.",
        "Set-PSReadLineOption -AddToHistoryHandler {",
        "    param($line)",
        '    $flat = ($line -replace "`r?`n", " ").Replace("`t", " ")',
        '    $record = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss") + "`t" + '
        '$PWD.Path + "`t" + $flat',
        '    Add-Content -LiteralPath "' + log_file + '" -Value $record -Encoding UTF8',
        "    return $true",
        "}",
    ]
    return "\n".join(lines) + "\n"


def install_snippets(config):
    """--install-snippets: writes the snippet file, never touches $PROFILE."""
    path = os.path.join(SCRIPT_DIR, "histex_profile.ps1")
    with open(path, "w", encoding="utf-8-sig", newline="") as handle:
        handle.write(profile_snippet())
    profile = run_powershell("$PROFILE").strip() or "$PROFILE"
    print("Snippet written to: %s" % path)
    print("")
    print("Nothing was changed on your system. To enable it, add this single")
    print("line to your PowerShell profile (%s):" % profile)
    print("")
    print('   . "%s"' % path)
    print("")
    print("Then open a new terminal. To undo: delete the file and the line.")
    return 0


def sidecar_entries():
    """Reads the sidecar log written by the profile snippet (idea 23)."""
    path = os.path.join(SCRIPT_DIR, "history_log.tsv")
    if not os.path.isfile(path):
        return []
    records = []
    try:
        with open(path, "r", encoding="utf-8-sig", errors="replace") as handle:
            for line in handle:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 3:
                    records.append((parts[0], parts[1], parts[2]))
    except OSError:
        return []
    return records


def filter_by_sidecar(entries, today=False, here=False):
    """Keeps only entries logged today / in the current folder (idea 23)."""
    if not (today or here):
        return entries
    records = sidecar_entries()
    if not records:
        print("[i] no sidecar log yet - run `histex --install-snippets`, add the "
              "line to $PROFILE and open a new terminal.", file=sys.stderr)
        return []
    stamp = time.strftime("%Y-%m-%d")
    wanted = set()
    cwd = os.path.normcase(os.getcwd())
    for when, directory, command in records:
        if today and not when.startswith(stamp):
            continue
        if here and os.path.normcase(directory) != cwd:
            continue
        wanted.add(normalize_entry(command))
    return [entry for entry in entries if normalize_entry(entry) in wanted]


def emit_selection(selected, as_json=False):
    """--pick / --json: machine readable output (ideas 2 and 25)."""
    if as_json:
        print(json.dumps({"count": len(selected), "commands": selected},
                         ensure_ascii=False))
    else:
        sys.stdout.write("\n".join(selected))
    return 0


# --- self test (idea 29) ----------------------------------------------------
def self_test(config):
    """--self-test: fast, offline checks of the parsing and helper logic."""
    results = []

    def check(name, condition):
        results.append((name, bool(condition)))

    check("dedup keeps the newest variant first",
          dedup_entries(["a", "b", "a"]) == ["a", "b"])
    check("near-duplicates merge",
          normalize_entry("git   status") == normalize_entry("git status"))
    check("noise: 'cls' is dropped", is_noise("cls", DEFAULT_CONFIG["exclude"]))
    check("noise: 'git status' is kept",
          not is_noise("git status", DEFAULT_CONFIG["exclude"]))
    check("multi-line commands are rejoined",
          entries_from_text("if ($x) {\n  Write-Host 1\n}") == ["if ($x) {\n  Write-Host 1\n}"])
    check("balanced lines stay separate",
          entries_from_text("line one\nline two") == ["line one", "line two"])
    check("pipeline split", split_pipeline("a | b; c && d") == ["a", "b", "c", "d"])
    check("pipeline honours quotes",
          split_pipeline('echo "a | b"') == ['echo "a | b"'])
    check("flags", flags_in("tar -xzf f.tgz --verbose") == ["--verbose", "-xzf"])
    check("query candidates",
          candidates("git status -sb", DEFAULT_CONFIG, resolve=False) == ["git status", "git"])
    check("slugify", slugify("Local LLM Setup!") == "local-llm-setup")
    check("ps1 script body", script_body(["ls"], ".ps1").endswith("ls\n"))
    check("bat script body", script_body(["ls"], ".bat").startswith("@echo off"))
    check("bat script body uses CRLF", "\r\n" in script_body(["ls"], ".bat"))
    check("NUL separated list", list_text(["a", "b"]) == "a\0b\0")
    check("secret guard", is_secret("mysql -p password=1", DEFAULT_CONFIG) is not None)
    check("danger guard", danger_reason("rm -rf /tmp/x", DEFAULT_CONFIG) is not None)
    check("danger guard leaves safe commands alone",
          danger_reason("ls -la", DEFAULT_CONFIG) is None)
    check("cheat.sh miss detection", cheat_miss("# 404 NOT FOUND"))
    check("cheat.sh hit detection", not cheat_miss("# tar\n# GNU version"))
    check("fzf install hint is OS specific", "install" in fzf_install_hint())
    check("explain order includes the local tldr source",
          "tldr" in (DEFAULT_CONFIG.get("explain_order") or []))
    check("local tldr source can be disabled",
          tldr_lookup("zzz-not-real", dict(DEFAULT_CONFIG, tldr=False)) is None)
    check("tldr path lookup is safe",
          tldr_path() is None or os.path.isfile(tldr_path()))
    check("tldr update helper is wired", callable(update_tldr))
    preview = preview_text("zzz-cmd | ww; qq", DEFAULT_CONFIG)
    check("preview keeps compound commands whole", "--- [1/" not in preview)
    check("preview shows the whole command",
          "zzz-cmd" in preview.splitlines()[0])
    check("preview counts the joined commands",
          any("3 commands joined" in line for line in preview.splitlines()))
    sample = ("### Date: 2026-10-04 00:39 - My title   <!-- tags: a, b -->\n"
              "\n```bash\nls -la\n```\n")
    check("recipe parsing strips date and tags",
          parse_recipes(DEFAULT_CONFIG, sample) == [("My title", ["a", "b"], ["ls -la"])])
    check("preview is hidden by default",
          DEFAULT_CONFIG.get("preview") is False)
    check("preview toggle key is configured",
          (DEFAULT_CONFIG.get("preview_key") or "") == "ctrl-p")
    check("header mentions the preview key",
          "^P" in FZF_HEADER)

    failures = [item for item in results if not item[1]]
    for name, ok in results:
        print("%s %s" % ("[ok]  " if ok else "[FAIL]", name))
    print("")
    print("%d/%d checks passed" % (len(results) - len(failures), len(results)))
    return 1 if failures else 0


# --- command line -----------------------------------------------------------
def build_parser():
    parser = argparse.ArgumentParser(
        prog="histex",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        description="Interactive picker for shell command history (fzf based).",
        epilog=("Keys inside fzf:\n"
                "  ENTER   explain            TAB     mark / unmark\n"
                "  CTRL-T  save recipe        CTRL-O  copy to clipboard\n"
                "  CTRL-P  toggle preview     CTRL-R  reload + sort toggle"))
    parser.add_argument("--version", action="version", version="histex " + VERSION)
    parser.add_argument("-r", "--recipes", metavar="FILE", help="recipes markdown file")
    parser.add_argument("--scripts-dir", metavar="DIR", help="where runnable scripts go")
    parser.add_argument("--jsonl", metavar="FILE", help="machine readable recipe log")
    parser.add_argument("--config", metavar="FILE",
                        help="config file (default: %s)" % CONFIG_PATH)
    parser.add_argument("--history", metavar="FILE", help="history file to read")
    parser.add_argument("--shell", choices=["auto", "ps5", "ps7", "bash", "zsh"],
                        help="which shell history to read")
    parser.add_argument("--sort", choices=["recent", "freq"], help="initial ordering")
    parser.add_argument("--detail", choices=["short", "full"], help="explanation depth")
    parser.add_argument("--max-items", type=int, help="cap the number of entries")
    parser.add_argument("--today", action="store_true",
                        help="only commands logged today (needs the profile snippet)")
    parser.add_argument("--here", action="store_true",
                        help="only commands run in this folder (needs the snippet)")
    parser.add_argument("--offline", action="store_true", help="never use the network")
    parser.add_argument("--no-cache", action="store_true", help="ignore the local cache")
    parser.add_argument("--no-preview", action="store_true",
                        help="disable the preview pane entirely (no toggle)")
    parser.add_argument("--show-preview", action="store_true",
                        help="start with the preview pane visible")
    parser.add_argument("--no-tldr", action="store_true",
                        help="do not use the local tldr pages")
    parser.add_argument("--update-tldr", action="store_true",
                        help="refresh the local tldr page cache (tldr --update)")
    parser.add_argument("--browse", action="store_true", help="browse the saved recipes")
    parser.add_argument("--clean", action="store_true", help="delete history entries")
    parser.add_argument("--stats", action="store_true", help="show usage statistics")
    parser.add_argument("--pick", action="store_true",
                        help="print only the chosen command(s) (shell integration)")
    parser.add_argument("--json", action="store_true", help="machine readable output")
    parser.add_argument("--explain", metavar="CMD", help="explain a command and exit")
    parser.add_argument("--preview", metavar="CMD", help=argparse.SUPPRESS)
    parser.add_argument("--print-list", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--toggle-sort", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--init-config", action="store_true", help="write a config file")
    parser.add_argument("--install-snippets", action="store_true",
                        help="write a PowerShell profile snippet file")
    parser.add_argument("--self-test", action="store_true", help="run offline self checks")
    return parser


def main(argv=None):
    setup_console()
    options = build_parser().parse_args(argv)
    config = load_config(options.config)

    for attribute, key in (("recipes", "recipes"), ("scripts_dir", "scripts_dir"),
                           ("jsonl", "jsonl"),
                           ("history", "history"), ("shell", "shell"),
                           ("sort", "sort"), ("detail", "detail"),
                           ("max_items", "max_items")):
        value = getattr(options, attribute, None)
        if value is not None:
            config[key] = value
    if options.offline:
        config["network"] = False
    if options.no_cache:
        config["cache"] = False
    if options.no_preview:
        config["preview"] = False
        config["_preview_off"] = True
    if options.show_preview:
        config["preview"] = True
    if options.no_tldr:
        config["tldr"] = False

    if options.init_config:
        path = write_default_config(options.config or CONFIG_PATH)
        print("[ok] config written to: %s" % path)
        return 0
    if options.install_snippets:
        return install_snippets(config)
    if options.self_test:
        return self_test(config)
    if options.update_tldr:
        return update_tldr()
    if options.preview is not None:
        explain(options.preview, config, preview=True)
        return 0
    if options.explain:
        explain(options.explain, config)
        return 0
    if options.print_list:
        print_list(config, do_toggle=options.toggle_sort)
        return 0
    if options.browse:
        return recipes_mode(config)
    if options.stats:
        return stats_mode(config)
    if options.clean:
        return clean_mode(config)

    label, path, entries = load_history(config)
    if not path:
        print("[x] no history file found. Looked in:", file=sys.stderr)
        for name, candidate in history_candidates():
            print("    %-4s %s" % (name, candidate), file=sys.stderr)
        print("[i] run a few commands in your shell and try again.", file=sys.stderr)
        return 1
    if options.today or options.here:
        entries = filter_by_sidecar(entries, today=options.today, here=options.here)
    if not entries:
        print("[i] nothing to show.", file=sys.stderr)
        return 0

    print("[i] %s   (%s)" % (path, label), file=sys.stderr)
    print("    %d unique commands, sorted by %s" % (len(entries), config.get("sort")),
          file=sys.stderr)
    print("    keys: ENTER explain | TAB mark | CTRL-T save | CTRL-O copy | "
          "CTRL-P preview | CTRL-R reload/sort | ESC cancel", file=sys.stderr)

    status, key, selected = run_fzf(entries, config)
    if status == "error":
        return 1
    if status == "cancel":
        print("[i] cancelled.")
        return 0
    if not selected:
        print("[i] nothing selected.")
        return 0

    if options.pick or options.json:
        return emit_selection(selected, as_json=options.json)

    if key == "ctrl-o":
        do_clipboard(selected)
        return 0
    if key in ("ctrl-t", "ctrl-x"):
        save_recipe_flow(selected, config)
        return 0

    for command in selected:
        reason = danger_reason(command, config)
        if reason:
            print("[!] destructive pattern '%s' - be careful before running this."
                  % reason, file=sys.stderr)
    for index, command in enumerate(selected):
        if index:
            print("")
            print("-" * 60)
        explain(command, config)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("\n[i] interrupted.", file=sys.stderr)
        sys.exit(130)
