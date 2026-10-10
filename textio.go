package main

// textio.go - text I/O that mirrors what the Python version did on the wire.
//
// CPython opens text files and stdout/stderr with newline=None. That means:
//   * on write, every "\n" becomes os.linesep (so "\r\n" on Windows, and an
//     existing "\r\n" becomes "\r\r\n"),
//   * on read, "\r\n" and a lone "\r" are folded back to "\n".
// The Python histex relied on this (its saved recipes, config, cache and
// migrated.json are all CRLF on Windows), so the port keeps the same bytes.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var windowsNewlines = runtime.GOOS == "windows"

// stdoutIsPayload is set by --pick / --json: stdout then carries only the
// machine payload (the selection or the JSON), and every human-facing line
// (prompts, info, headers) is routed to stderr. The shell wrappers capture
// stdout (`$picked = & histex --pick`), so chatter on stdout would end up
// pasted into the prompt - keeping the payload stream clean is the contract
// the wrappers rely on.
var stdoutIsPayload bool

// payloadAsJSON is set by --json: the payload is JSON instead of plain text.
var payloadAsJSON bool

// humanTarget is where human-facing text goes: stdout normally, stderr while
// stdout is reserved for the payload.
func humanTarget() io.Writer {
	if stdoutIsPayload {
		return os.Stderr
	}
	return os.Stdout
}

// translateNewlines reproduces Python's text-mode write translation.
func translateNewlines(text string) string {
	if !windowsNewlines {
		return text
	}
	return strings.ReplaceAll(text, "\n", "\r\n")
}

// universalNewlines reproduces Python's text-mode read translation.
func universalNewlines(text string) string {
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// outLine writes one line of human-facing text the way print() did:
// to stdout normally, to stderr while stdout is reserved for the payload.
func outLine(format string, args ...any) {
	io.WriteString(humanTarget(), translateNewlines(sprintf(format, args...)+"\n"))
}

// outRaw writes human-facing text exactly (newline translation only), routed
// like outLine.
func outRaw(text string) {
	io.WriteString(humanTarget(), translateNewlines(text))
}

// payloadLine writes one line to stdout unconditionally: this is the machine
// payload (--pick selection, --json report) that shell wrappers capture.
func payloadLine(text string) {
	io.WriteString(os.Stdout, translateNewlines(text))
}

// errLine writes one line to stderr the way print(file=sys.stderr) did.
func errLine(format string, args ...any) {
	io.WriteString(os.Stderr, translateNewlines(sprintf(format, args...)+"\n"))
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// pySplitLines matches str.splitlines(): it splits on every line boundary
// Python knows about, and a trailing boundary does not create an empty item.
func pySplitLines(text string) []string {
	var out []string
	var current strings.Builder
	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\x85', '\u2028', '\u2029':
			out = append(out, current.String())
			current.Reset()
		case '\r':
			if index+1 < len(runes) && runes[index+1] == '\n' {
				index++
			}
			out = append(out, current.String())
			current.Reset()
		default:
			current.WriteRune(runes[index])
		}
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// stringWidth returns the character count (Python len on str).
func stringWidth(text string) int {
	return len([]rune(text))
}

// truncateChars keeps at most count characters (Python slicing).
func truncateChars(text string, count int) string {
	runes := []rune(text)
	if len(runes) <= count {
		return text
	}
	return string(runes[:count])
}

// nowSeconds matches time.time().
func nowSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return os.Getenv("USERPROFILE")
}

// expandUser matches os.path.expanduser for the "~" / "~/" forms.
func expandUser(path string) string {
	if path == "~" {
		return homeDir()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

// absPath matches os.path.abspath(os.path.expanduser(value)).
func absPath(value string) string {
	expanded := expandUser(value)
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return expanded
	}
	return absolute
}

// normCase matches os.path.normcase (lowercase + backslashes on Windows).
func normCase(path string) string {
	if !windowsNewlines {
		return path
	}
	return strings.ToLower(strings.ReplaceAll(path, "/", "\\"))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// fileModTime matches os.path.getmtime.
func fileModTime(path string) (float64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return float64(info.ModTime().UnixNano()) / 1e9, true
}

func ensureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o777)
}

// decodeText mirrors open(..., encoding=..., errors="replace") + BOM handling.
func decodeText(raw []byte, stripBOM bool) string {
	if stripBOM && len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		raw = raw[3:]
	}
	return strings.ToValidUTF8(string(raw), "\uFFFD")
}

// readTextFile reads with universal newlines; bom strips a UTF-8 BOM.
func readTextFile(path string, bom bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return universalNewlines(decodeText(raw, bom)), nil
}

// writeTextFile mirrors open(path, "w", encoding=..., newline=newline).
// translate=false means "no translation" (Python's explicit newline="").
func writeTextFile(path string, text string, bom bool, translate bool) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if translate {
		text = translateNewlines(text)
	}
	payload := make([]byte, 0, len(text)+3)
	if bom {
		payload = append(payload, 0xEF, 0xBB, 0xBF)
	}
	payload = append(payload, text...)
	return os.WriteFile(path, payload, 0o666)
}

// appendTextFile mirrors open(path, "a", encoding=...): a BOM is only written
// when the file is created by this call, exactly like the utf-8-sig codec.
func appendTextFile(path string, text string, bom bool, translate bool) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if translate {
		text = translateNewlines(text)
	}
	exists := isFile(path)
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	handle, err := os.OpenFile(path, flags, 0o666)
	if err != nil {
		return err
	}
	defer handle.Close()
	if bom && !exists {
		if _, err := handle.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
	}
	_, err = io.WriteString(handle, text)
	return err
}

// atomicWrite is the ported atomic_write(): text, encoding utf-8 and
// newline=None, so newline translation follows the OS like Python did.
func atomicWrite(path string, text string) error {
	return atomicWriteBytes(path, []byte(translateNewlines(text)))
}

// atomicWriteRaw is atomic_write(path, "", raw=bytes): no translation.
func atomicWriteRaw(path string, raw []byte) error {
	return atomicWriteBytes(path, raw)
}

func atomicWriteBytes(path string, raw []byte) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// copyFilePlain mirrors shutil.copyfile.
func copyFilePlain(source string, target string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(target)); err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o666)
}

// pyURLQuote matches urllib.parse.quote(value, safe="").
func pyURLQuote(value string) string {
	const safe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-~"
	var b strings.Builder
	for _, octet := range []byte(value) {
		if strings.IndexByte(safe, octet) >= 0 {
			b.WriteByte(octet)
			continue
		}
		b.WriteByte('%')
		b.WriteString(strings.ToUpper(strconv.FormatUint(uint64(octet), 16)))
	}
	return b.String()
}
