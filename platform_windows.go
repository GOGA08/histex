//go:build windows

package main

// platform_windows.go - the Windows specific pieces.

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

// setupConsole makes the console render UTF-8. Python reconfigured its streams
// to UTF-8; a Go program writes bytes, so the code page has to follow.
func setupConsole() {
	setConsoleOutputCP.Call(65001)
}

// runPowerShell is run_powershell(): Windows only, "" on any failure.
func runPowerShell(script string, timeout int) string {
	exe := lookWhich("powershell", "pwsh")
	if exe == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive",
		"-Command", script)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ""
		}
		// A non-zero exit still leaves usable stdout for Python, so keep it.
		if out.Len() == 0 {
			return ""
		}
	}
	return decodeText(out.Bytes(), false)
}

// clipUnicodeWindows is _clip_unicode_windows(): Set-Clipboard via PowerShell.
func clipUnicodeWindows(text string) bool {
	exe := lookWhich("powershell", "pwsh")
	if exe == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive",
		"-Command", "Set-Clipboard -Value $args[0]", text)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// clipboardCopy is clipboard_copy(): ASCII is fast, Unicode is safe.
func clipboardCopy(text string) bool {
	if text == "" {
		return false
	}
	if isASCII(text) {
		return runPipe([]string{"clip"}, text)
	}
	return clipUnicodeWindows(text)
}

// fzfInstallHint is fzf_install_hint() for Windows.
func fzfInstallHint() string {
	return "    winget install junegunn.fzf     " +
		"(alternatives: scoop install fzf | choco install fzf)"
}

// tldrInstallHint is the Windows hint used by --update-tldr.
func tldrInstallHint() string {
	return "    winget install dbrgn.tealdeer"
}
