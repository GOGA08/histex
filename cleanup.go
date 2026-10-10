package main

// cleanup.go - the temporary files that must not outlive the process.
//
// --browse hands the prepared recipe list and the preview map to the picker
// through temp files, and the normal path removes them with defer. CTRL-C is
// the exception: the interrupt handler exits with code 130 right away, so no
// defer runs and the two small files stay behind in %TEMP%. They are
// registered here as well, so the interrupt handler (and the last line of
// main) can drop them too. Removing an already removed file is harmless, so
// the deferred removal and the exit-time sweep simply cooperate.

import (
	"os"
	"sync"
)

var (
	tempFilesMu sync.Mutex
	tempFiles   []string
)

// trackTempFile remembers a temp file for the exit-time cleanup.
func trackTempFile(path string) {
	tempFilesMu.Lock()
	defer tempFilesMu.Unlock()
	tempFiles = append(tempFiles, path)
}

// newTrackedTemp is os.CreateTemp plus the cleanup registration, so a CTRL-C
// cannot leave the file behind.
func newTrackedTemp(pattern string) (*os.File, error) {
	handle, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}
	trackTempFile(handle.Name())
	return handle, nil
}

// cleanupTempFiles removes every registered temp file. It is safe to call
// twice: a file the deferred removal already took care of is skipped.
func cleanupTempFiles() {
	tempFilesMu.Lock()
	pending := tempFiles
	tempFiles = nil
	tempFilesMu.Unlock()
	for _, path := range pending {
		os.Remove(path)
	}
}
