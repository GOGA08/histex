package main

// fzf.go - the picker: launching fzf, the NUL protocol and sort toggling.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FZF_HEADER - the one line pinned on top of the picker.
const fzfHeader = "ENTER explain | TAB mark | ^T save | ^O copy | ^P preview | ^R sort"

// fzfPath is fzf_path(). A copy next to histex.exe wins over PATH: the Windows
// release ships one, so histex needs no separate fzf install.
func fzfPath() string {
	for _, name := range []string{"fzf.exe", "fzf"} {
		candidate := filepath.Join(scriptDir, name)
		if isFile(candidate) {
			return candidate
		}
	}
	return lookWhich("fzf")
}

// fzfInfo is the parsed result of `fzf --version`.
type fzfInfo struct {
	version      string
	major, minor int
}

// supportsHistoryScheme reports whether --scheme=history is available. The
// scheme arrived in fzf 0.33.0; older builds (Ubuntu 22.04 ships 0.29) reject
// the flag and would fail to start.
func (info fzfInfo) supportsHistoryScheme() bool {
	if info.major != 0 {
		return info.major > 0
	}
	return info.minor >= 33
}

// parseFzfVersion reads the first field of `fzf --version`, which looks like
// "0.74.4 (a140afeb)" - the hash part is optional.
func parseFzfVersion(text string) (fzfInfo, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return fzfInfo{}, false
	}
	number := strings.TrimPrefix(fields[0], "v")
	parts := strings.Split(number, ".")
	if len(parts) < 2 {
		return fzfInfo{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return fzfInfo{}, false
	}
	return fzfInfo{version: number, major: major, minor: minor}, true
}

// fzfVersion asks the fzf binary for its version. Probing costs one extra
// process (~10 ms) and histex runs are short, so the answer is cached for the
// rest of this process - at most one probe per run.
var (
	fzfProbeDone bool
	fzfProbeInfo fzfInfo
	fzfProbeOK   bool
)

func fzfVersion(exe string) (fzfInfo, bool) {
	if fzfProbeDone {
		return fzfProbeInfo, fzfProbeOK
	}
	fzfProbeDone = true
	cmd := exec.Command(exe, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fzfInfo{}, false
	}
	fzfProbeInfo, fzfProbeOK = parseFzfVersion(out.String())
	return fzfProbeInfo, fzfProbeOK
}

// executablePath is what self_command() used as sys.executable.
func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return appName
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// quoted is _quoted().
func quoted(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "") + "\""
}

// selfCommand is self_command(): the line that re-invokes this program for
// fzf's reload / preview binds. A Go binary is always its own executable, so
// there is no "script or frozen exe" branch to make: the exe path is enough.
func selfCommand(extra ...string) string {
	parts := append([]string{quoted(executablePath())}, extra...)
	return strings.Join(parts, " ")
}

// listText is list_text(): NUL separated, so multi-line commands survive.
func listText(entries []string) string {
	return strings.Join(entries, "\x00") + "\x00"
}

// toggleSort is toggle_sort(): flips recent <-> freq and remembers it.
func toggleSort(cfg *Config) string {
	current := cfg.Sort
	if current == "" {
		current = "recent"
	}
	statePath := sortStatePath(cfg)
	if text, err := readTextFile(statePath, false); err == nil {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			current = trimmed
		}
	}
	newMode := "freq"
	if current == "freq" {
		newMode = "recent"
	}
	if err := atomicWrite(statePath, newMode); err != nil {
		// stdout may feed fzf (the reload bind), so this goes to stderr.
		errLine("[!] could not save the sort state: %s", err)
	}
	return newMode
}

// historyReloadArgs is what the history picker's reload bind runs: the list,
// plus the filters that were active when the picker opened. The reload is a new
// process, so the filters have to travel on its command line.
func historyReloadArgs(filter []string) []string {
	return append([]string{"--print-list", "--toggle-sort"}, filter...)
}

// recipeReloadEntries returns the recipe library's prepared list, or
// (nil, false) when the reload is a plain history reload. The list is built by
// the parent once and handed over through HISTEX_RECIPES_LIST, so the reload
// shows the exact same lines - same titles, same duplicate disambiguation.
func recipeReloadEntries() ([]string, bool) {
	path := os.Getenv("HISTEX_RECIPES_LIST")
	if path == "" {
		return nil, false
	}
	text, err := readTextFile(path, false)
	if err != nil {
		return []string{}, true // the marker is set but the file is gone
	}
	return strings.Split(strings.TrimRight(text, "\x00"), "\x00"), true
}

// printList is print_list(): the ready-to-use list for fzf's reload bind. It
// has to re-apply --today / --here, otherwise a reload would quietly show the
// whole history again, and in the recipe library it prints the prepared recipe
// list instead of the history.
func printList(cfg *Config, doToggle bool, today bool, here bool) []string {
	if entries, isRecipes := recipeReloadEntries(); isRecipes {
		outRaw(listText(entries))
		return entries
	}
	if doToggle {
		local := *cfg
		local.Sort = toggleSort(cfg)
		cfg = &local
	}
	_, _, entries, err := loadHistory(cfg, true)
	if err != nil {
		// stdout feeds fzf, so the complaint has to go to stderr.
		errLine("[!] could not read the history: %s", err)
	}
	if today || here {
		entries = filterBySidecar(entries, today, here, cfg)
	}
	outRaw(listText(entries))
	return entries
}

// doClipboard is do_clipboard(): the CTRL-O action.
func doClipboard(selected []string) bool {
	text := strings.Join(selected, "\n")
	if clipboardCopy(text) {
		outLine("[ok] copied %d command(s) to the clipboard.", len(selected))
		return true
	}
	errLine("[x] could not reach the clipboard.")
	return false
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

// runFzf is run_fzf(): opens the picker, returns (status, key, selected).
// reload is the command the reload bind runs, after the program path: the
// history picker passes its filters, the recipe library its own list command.
func runFzf(entries []string, cfg *Config, allowPreview bool, prompt string, reload []string) (string, string, []string) {
	exe := fzfPath()
	if exe == "" {
		errLine("[x] fzf not found in PATH - histex requires fzf.")
		errLine("    Install it with:")
		errLine("%s", fzfInstallHint())
		errLine("    Then open a new terminal so PATH is refreshed.")
		return "error", "", []string{}
	}

	expect := []string{}
	for _, key := range strings.Split(cfg.Expect, ",") {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			expect = append(expect, trimmed)
		}
	}
	if !containsString(expect, "ctrl-o") {
		expect = append(expect, "ctrl-o")
	}

	window := cfg.PreviewWindow
	if window == "" {
		window = "right:40%:wrap"
	}
	previewKey := strings.TrimSpace(cfg.PreviewKey)
	if previewKey == "" {
		previewKey = "ctrl-p"
	}
	reloadKey := cfg.ReloadKey
	if reloadKey == "" {
		reloadKey = "ctrl-r"
	}
	if prompt == "" {
		prompt = "histex> "
	}

	args := []string{
		exe,
		"--multi",
		"--expect=" + strings.Join(expect, ","),
		"--read0",
		"--print0",
	}
	// --scheme=history needs fzf >= 0.33; older builds (Ubuntu 22.04 has
	// 0.29) reject the flag, so it is only passed when it is supported.
	if info, known := fzfVersion(exe); known {
		if info.supportsHistoryScheme() {
			args = append(args, "--scheme=history")
		} else {
			errLine("[i] fzf %s is older than 0.33 - picking with the default "+
				"scoring scheme (install a newer fzf for history ranking).",
				info.version)
		}
	} else {
		errLine("[i] could not read the fzf version - using the default " +
			"scoring scheme.")
	}
	args = append(args,
		"--height=80%",
		"--border",
		"--layout=reverse",
		"--marker=> ",
		"--pointer=>",
		"--prompt="+prompt,
		"--header="+fzfHeader,
		"--header-first",
		"--bind="+reloadKey+":reload("+selfCommand(reload...)+")",
	)
	if allowPreview && !cfg.PreviewOff {
		if !cfg.Preview && !strings.Contains(window, "hidden") {
			window += ",hidden" // hidden by default; toggle it with CTRL-P
		}
		args = append(args, "--preview="+selfCommand("--preview", "{}"))
		args = append(args, "--preview-window="+window)
		args = append(args, "--bind="+previewKey+":toggle-preview")
		if previewKey != "ctrl-/" {
			args = append(args, "--bind=ctrl-/:toggle-preview")
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	// subprocess wrote text, so newline translation applied to the pipe too.
	cmd.Stdin = strings.NewReader(translateNewlines(listText(entries)))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code == 1 || code == 130 { // 1 = no match, 130 = ESC / CTRL-C
				return "cancel", "", []string{}
			}
			errLine("[x] fzf exited with code %s", itoa(code))
			if text := strings.TrimSpace(stderr.String()); text != "" {
				errLine("%s", text)
			}
			return "error", "", []string{}
		}
		errLine("[x] could not start fzf: %s", err)
		return "error", "", []string{}
	}

	// --print0: every field is NUL terminated. With --expect the very first
	// field is the pressed key, and that field is EMPTY for ENTER - so empty
	// fields must not be filtered out before the key has been read.
	outText := universalNewlines(decodeText(stdout.Bytes(), false))
	fields := strings.Split(outText, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	key := ""
	if len(fields) > 0 && fields[0] == "" {
		fields = fields[1:] // explicit empty key field = ENTER
	} else if len(fields) > 0 && containsString(expect, fields[0]) {
		key = fields[0]
		fields = fields[1:]
	}
	return "ok", key, fields
}
