package main

// exec.go - small helpers around os/exec that both platforms share.

import (
	"os/exec"
	"strings"
)

// lookWhich mirrors shutil.which over a list of candidates.
func lookWhich(names ...string) string {
	for _, name := range names {
		if found, err := exec.LookPath(name); err == nil && found != "" {
			return found
		}
	}
	return ""
}

// runPipe mirrors _run_pipe(): stdin gets the text, output is discarded and
// only the exit status matters.
func runPipe(command []string, text string) bool {
	if len(command) == 0 {
		return false
	}
	exe, err := exec.LookPath(command[0])
	if err != nil {
		return false
	}
	cmd := exec.Command(exe, command[1:]...)
	cmd.Stdin = strings.NewReader(text)
	// nil Stdout/Stderr means the null device, like subprocess.DEVNULL.
	return cmd.Run() == nil
}

// isASCII mirrors str.isascii().
func isASCII(text string) bool {
	for _, r := range text {
		if r > 0x7F {
			return false
		}
	}
	return true
}
