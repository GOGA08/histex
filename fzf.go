package main

// fzf.go - the picker: launching fzf, the NUL protocol and sort toggling.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FZF_HEADER - the one line pinned on top of the picker.
const fzfHeader = "ENTER explain | TAB mark | ^T save | ^O copy | ^P preview | ^R sort"

// fzfPath is fzf_path().
func fzfPath() string {
	return lookWhich("fzf")
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
// fzf's reload / preview binds. A compiled binary is always its own exe, so
// the script path the Python version had to add when not frozen is omitted.
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
	atomicWrite(statePath, newMode)
	return newMode
}

// printList is print_list(): the ready-to-use list for fzf's reload bind.
func printList(cfg *Config, doToggle bool) []string {
	if doToggle {
		local := *cfg
		local.Sort = toggleSort(cfg)
		cfg = &local
	}
	_, _, entries, _ := loadHistory(cfg, true)
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
func runFzf(entries []string, cfg *Config, allowPreview bool, prompt string) (string, string, []string) {
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
		"--scheme=history",
		"--height=80%",
		"--border",
		"--layout=reverse",
		"--marker=> ",
		"--pointer=>",
		"--prompt=" + prompt,
		"--header=" + fzfHeader,
		"--header-first",
		"--bind=" + reloadKey + ":reload(" + selfCommand("--print-list", "--toggle-sort") + ")",
	}
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
