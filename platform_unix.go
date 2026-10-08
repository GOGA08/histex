//go:build !windows

package main

// platform_unix.go - the macOS / Linux specific pieces.

import (
	"runtime"
)

// setupConsole has nothing to do away from Windows: stdout is already UTF-8.
func setupConsole() {}

// runPowerShell is run_powershell(): it only ever ran on Windows, so this
// stays empty off Windows, exactly like the Python version.
func runPowerShell(script string, timeout int) string {
	return ""
}

// clipboardCopy is clipboard_copy() for macOS and Linux.
func clipboardCopy(text string) bool {
	if text == "" {
		return false
	}
	if runtime.GOOS == "darwin" {
		return runPipe([]string{"pbcopy"}, text)
	}
	for _, command := range [][]string{
		{"xclip", "-selection", "clipboard"},
		{"wl-copy"},
	} {
		if lookWhich(command[0]) != "" && runPipe(command, text) {
			return true
		}
	}
	return false
}

// fzfInstallHint is fzf_install_hint() for macOS and Linux.
func fzfInstallHint() string {
	if runtime.GOOS == "darwin" {
		return "    brew install fzf"
	}
	return "    sudo apt install fzf     (or: sudo dnf install fzf | pacman -S fzf)"
}

// tldrInstallHint is the macOS / Linux hint used by --update-tldr.
func tldrInstallHint() string {
	if runtime.GOOS == "darwin" {
		return "    brew install tealdeer"
	}
	return "    cargo install tealdeer   (or: sudo apt install tealdeer)"
}
