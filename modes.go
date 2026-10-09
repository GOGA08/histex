package main

// modes.go - --clean, --stats, the sidecar log and the prompt integration.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// rootOrScriptDir mirrors `config.get("_datadir") or SCRIPT_DIR`.
func rootOrScriptDir(cfg *Config) string {
	if cfg != nil && cfg.DataDir != "" {
		return cfg.DataDir
	}
	return scriptDir
}

// cleanMode is clean_mode(): --clean deletes selected history entries.
func cleanMode(cfg *Config) int {
	_, path, entries, err := loadHistory(cfg, false)
	if err != nil {
		errLine("[x] could not read the history file: %s", err)
		return 1
	}
	if path == "" {
		errLine("[x] no history file found.")
		return 1
	}
	status, _, selected := runFzf(entries, cfg, false, "clean> ")
	if status != "ok" || len(selected) == 0 {
		return 0
	}
	doomed := map[string]bool{}
	for _, item := range selected {
		doomed[item] = true
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		errLine("[x] could not read the history file: %s", err)
		return 1
	}
	hadBOM := len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF
	lines := pySplitLines(decodeText(raw, true))
	kept := []string{}
	for _, line := range lines {
		if !doomed[line] {
			kept = append(kept, line)
		}
	}
	removed := len(lines) - len(kept)
	if removed == 0 {
		outLine("[i] nothing matched - the file was left untouched.")
		return 0
	}
	backup := path + ".histex-backup"
	if err := copyFilePlain(path, backup); err != nil {
		errLine("[x] could not rewrite the history file: %s", err)
		return 1
	}
	// newline="" in Python: no newline translation on this rewrite.
	if err := writeTextFile(path, strings.Join(kept, "\n")+"\n", hadBOM, false); err != nil {
		errLine("[x] could not rewrite the history file: %s", err)
		return 1
	}
	outLine("[ok] removed %d line(s); backup kept at %s", removed, backup)
	return 0
}

// statsMode is stats_mode(): --stats shows the usage statistics.
func statsMode(cfg *Config) int {
	label, path, raw, err := rawEntries(cfg, true)
	if err != nil {
		errLine("[x] could not read the history file: %s", err)
		return 1
	}
	if path == "" {
		errLine("[x] no history file found.")
		return 1
	}
	unique := dedupEntries(raw)
	outLine("history file : %s   (%s)", path, label)
	outLine("entries      : %d total, %d unique", len(raw), len(unique))
	outLine("")
	outLine("top 15 commands:")
	for index, pair := range orderedCounts(raw) {
		if index >= 15 {
			break
		}
		outLine("   %4d x  %s", pair.count, truncateChars(firstLine(pair.key), 70))
	}
	tools := map[string]int{}
	toolOrder := []string{}
	for _, entry := range raw {
		name := ""
		if fields := strings.Fields(entry); len(fields) > 0 {
			name = strings.ToLower(normalizeToken(fields[0]))
		}
		if name == "" {
			continue
		}
		if _, seen := tools[name]; !seen {
			toolOrder = append(toolOrder, name)
		}
		tools[name]++
	}
	toolPairs := make([]countPair, 0, len(toolOrder))
	for _, name := range toolOrder {
		toolPairs = append(toolPairs, countPair{key: name, count: tools[name]})
	}
	sort.SliceStable(toolPairs, func(i, j int) bool {
		return toolPairs[i].count > toolPairs[j].count
	})
	outLine("")
	outLine("top 15 tools:")
	for index, pair := range toolPairs {
		if index >= 15 {
			break
		}
		outLine("   %4d x  %s", pair.count, pair.key)
	}
	if len(unique) != len(raw) {
		outLine("")
		outLine("noise removed from the picker: %d entries", len(raw)-len(unique))
	}
	outLine("")
	outLine("explain order : %s", strings.Join(cfg.ExplainOrder, " -> "))
	source := tldrPath()
	if source == "" {
		source = "not installed"
	}
	outLine("tldr source   : %s", source)
	return 0
}

type sidecarRecord struct {
	when    string
	dir     string
	command string
}

// sidecarEntries is sidecar_entries(): reads the snippet's sidecar log.
func sidecarEntries(cfg *Config) []sidecarRecord {
	path := filepath.Join(rootOrScriptDir(cfg), "history_log.tsv")
	if !isFile(path) {
		return []sidecarRecord{}
	}
	text, err := readTextFile(path, true)
	if err != nil {
		return []sidecarRecord{}
	}
	records := []sidecarRecord{}
	for _, line := range pySplitLines(text) {
		parts := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(parts) >= 3 {
			records = append(records, sidecarRecord{parts[0], parts[1], parts[2]})
		}
	}
	return records
}

// filterBySidecar is filter_by_sidecar(): today / this folder only.
func filterBySidecar(entries []string, today bool, here bool, cfg *Config) []string {
	if !today && !here {
		return entries
	}
	records := sidecarEntries(cfg)
	if len(records) == 0 {
		errLine("[i] no sidecar log yet - run `histex --install-snippets`, add " +
			"the line to $PROFILE and open a new terminal.")
		return []string{}
	}
	stamp := time.Now().Format("2006-01-02")
	wanted := map[string]bool{}
	cwd := normCase(mustGetwd())
	for _, record := range records {
		if today && !strings.HasPrefix(record.when, stamp) {
			continue
		}
		if here && normCase(record.dir) != cwd {
			continue
		}
		wanted[normalizeEntry(record.command)] = true
	}
	out := []string{}
	for _, entry := range entries {
		if wanted[normalizeEntry(entry)] {
			out = append(out, entry)
		}
	}
	return out
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// emitSelection is emit_selection(): --pick / --json output.
func emitSelection(selected []string, asJSON bool) int {
	if asJSON {
		outLine("%s", pyJSON(jobject{
			{"count", len(selected)},
			{"commands", selected},
		}, "", 0))
	} else {
		outRaw(strings.Join(selected, "\n"))
	}
	return 0
}
