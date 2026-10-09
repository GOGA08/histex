package main

// history.go - history discovery, parsing, cleaning, ordering and the cache.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type historySource struct {
	label string
	path  string
}

// historyCandidates is history_candidates(): every shell we know about.
func historyCandidates() []historySource {
	appdata := os.Getenv("APPDATA")
	var items []historySource
	if appdata != "" {
		// Windows PowerShell 5.1
		items = append(items, historySource{"ps5", filepath.Join(
			appdata, "Microsoft", "Windows", "PowerShell", "PSReadLine",
			"ConsoleHost_history.txt")})
		// PowerShell 7+
		items = append(items, historySource{"ps7", filepath.Join(
			appdata, "Microsoft", "PowerShell", "PSReadLine",
			"ConsoleHost_history.txt")})
	}
	// PowerShell 7+ on macOS / Linux
	items = append(items, historySource{"ps7", filepath.Join(
		homeDirPath, ".local", "share", "powershell", "PSReadLine",
		"ConsoleHost_history.txt")})
	items = append(items, historySource{"bash", filepath.Join(homeDirPath, ".bash_history")})
	items = append(items, historySource{"zsh", filepath.Join(homeDirPath, ".zsh_history")})
	items = append(items, historySource{"fish", fishHistoryPath()})
	return items
}

// fishHistoryPath is where fish keeps its history: $XDG_DATA_HOME or
// ~/.local/share on every platform, macOS included.
func fishHistoryPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "fish", "fish_history")
	}
	return filepath.Join(homeDirPath, ".local", "share", "fish", "fish_history")
}

// resolveHistory is resolve_history(): (label, path) of the used history file.
func resolveHistory(cfg *Config) (string, string) {
	if override := deref(cfg.History); override != "" {
		return "custom", absPath(override)
	}
	wanted := cfg.Shell
	if wanted == "" {
		wanted = "auto"
	}
	for _, source := range historyCandidates() {
		if wanted != "auto" && source.label != wanted {
			continue
		}
		if isFile(source.path) {
			return source.label, source.path
		}
	}
	return "", ""
}

// --- reading + cleaning the history -----------------------------------------

var zshPrefix = regexp.MustCompile(`^:\s*\d+:\d+;`)

// whitespaceRun matches what Python's \s+ matches for str (Unicode aware).
var whitespaceRun = regexp.MustCompile(`[\t\n\v\f\r \x{0085}\p{Zs}\p{Zl}\p{Zp}]+`)

var regexCache = map[string]*regexp.Regexp{}
var regexBroken = map[string]bool{}

// compileSearch mirrors re.search(pattern, text, re.IGNORECASE): a bad pattern
// is simply skipped, exactly like the Python version did on re.error.
func compileSearch(pattern string) *regexp.Regexp {
	key := "(?i)" + pattern
	if compiled, ok := regexCache[key]; ok {
		return compiled
	}
	if regexBroken[key] {
		return nil
	}
	compiled, err := regexp.Compile(key)
	if err != nil {
		regexBroken[key] = true
		// Go's regexp is RE2: no lookbehind and no backreferences. Say so once
		// per pattern instead of silently ignoring a config entry.
		errLine("[!] ignoring the pattern %q: %s", pattern, err)
		return nil
	}
	regexCache[key] = compiled
	return compiled
}

type balanceState struct {
	quote rune
	stack []rune
}

// trackBalance is _track_balance(): (), [], {} and quote balance for one line.
func trackBalance(line string, state *balanceState) {
	closing := map[rune]rune{')': '(', ']': '[', '}': '{'}
	runes := []rune(line)
	index := 0
	for index < len(runes) {
		char := runes[index]
		if state.quote != 0 {
			if char == '`' {
				index += 2
				continue
			}
			if char == state.quote {
				state.quote = 0
			}
			index++
			continue
		}
		switch {
		case char == '\'' || char == '"':
			state.quote = char
		case char == '`':
			index += 2
			continue
		case char == '(' || char == '[' || char == '{':
			state.stack = append(state.stack, char)
		default:
			if opener, ok := closing[char]; ok {
				if len(state.stack) > 0 && state.stack[len(state.stack)-1] == opener {
					state.stack = state.stack[:len(state.stack)-1]
				}
			}
		}
		index++
	}
}

func allDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// entriesFromText is entries_from_text(): rejoins multi-line commands.
func entriesFromText(text string, joinContinuations bool) []string {
	var lines []string
	for _, line := range pySplitLines(text) {
		// bash with HISTTIMEFORMAT writes "#<epoch>" marker lines
		if stringWidth(line) == 11 && strings.HasPrefix(line, "#") && allDigits(line[1:]) {
			continue
		}
		lines = append(lines, zshPrefix.ReplaceAllString(line, ""))
	}

	if !joinContinuations {
		out := []string{}
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
		return out
	}

	entries := []string{}
	var buffer []string
	state := &balanceState{}
	for _, line := range lines {
		if len(buffer) == 0 {
			if strings.TrimSpace(line) == "" {
				continue
			}
			buffer = []string{line}
			state = &balanceState{}
		} else {
			buffer = append(buffer, line)
		}
		trackBalance(line, state)
		trailing := strings.TrimRightFunc(line, unicode.IsSpace)
		if state.quote == 0 && len(state.stack) == 0 && !strings.HasSuffix(trailing, "`") {
			entries = append(entries, strings.Join(buffer, "\n"))
			buffer = nil
		}
	}
	if len(buffer) > 0 {
		entries = append(entries, strings.Join(buffer, "\n"))
	}
	out := []string{}
	for _, entry := range entries {
		if strings.TrimSpace(entry) != "" {
			out = append(out, entry)
		}
	}
	return out
}

// entriesForSource parses history text the way the shell that produced it
// writes it. fish stores structured YAML records, so it needs its own reader.
func entriesForSource(label string, text string, joinContinuations bool) []string {
	if label == "fish" {
		return fishHistoryEntries(text)
	}
	return entriesFromText(text, joinContinuations)
}

// fishHistoryEntries reads fish's history file. Each record looks like
//
//   - cmd: ls -la
//     when: 1699999999
//
// and a command containing newlines is written as an indented block:
//
//   - cmd: |
//     if true
//     echo hi
//     end
//     when: 1700000000
func fishHistoryEntries(text string) []string {
	entries := []string{}
	var block []string
	inBlock := false

	flush := func() {
		if len(block) == 0 {
			return
		}
		joined := strings.Join(block, "\n")
		block = nil
		if strings.TrimSpace(joined) != "" {
			entries = append(entries, strings.TrimRight(joined, "\n"))
		}
	}

	for _, line := range pySplitLines(text) {
		switch {
		case strings.HasPrefix(line, "- cmd: "):
			flush()
			value := line[len("- cmd: "):]
			switch strings.TrimSpace(value) {
			case "|", "|-", "|+", ">", ">-", ">+":
				inBlock = true
			default:
				inBlock = false
				block = []string{unquoteYAMLScalar(value)}
			}
		case strings.HasPrefix(line, "  when:"):
			inBlock = false
		case inBlock && strings.HasPrefix(line, "    "):
			block = append(block, line[4:])
		case inBlock && strings.TrimSpace(line) == "":
			block = append(block, "")
		default:
			// a separator line or an unknown key ends the current record
			if inBlock {
				flush()
			}
			inBlock = false
		}
	}
	flush()
	return entries
}

// unquoteYAMLScalar unwraps the two quoting styles fish's writer may use.
func unquoteYAMLScalar(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		inner := value[1 : len(value)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		return strings.ReplaceAll(inner, `\\`, `\`)
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], `''`, `'`)
	}
	return value
}

// normalizeEntry is normalize_entry(): whitespace runs collapse to one space.
func normalizeEntry(entry string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(entry, " "))
}

// dedupEntries is dedup_entries(): newest variant first, seen when reversed.
func dedupEntries(entries []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for index := len(entries) - 1; index >= 0; index-- {
		key := normalizeEntry(entries[index])
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, entries[index])
	}
	return result
}

// isNoise is is_noise(): True when any exclude pattern matches.
func isNoise(entry string, patterns []string) bool {
	stripped := strings.TrimSpace(entry)
	if stripped == "" {
		return true
	}
	for _, pattern := range patterns {
		compiled := compileSearch(pattern)
		if compiled == nil {
			continue
		}
		if compiled.MatchString(stripped) {
			return true
		}
	}
	return false
}

type countPair struct {
	key   string
	count int
}

// countEntries is count_entries(): normalized form -> how often it was used.
func countEntries(entries []string) map[string]int {
	counts := map[string]int{}
	for _, entry := range entries {
		key := normalizeEntry(entry)
		counts[key]++
	}
	return counts
}

// orderedCounts is sorted(counts.items(), key=lambda pair: -pair[1]) with
// Python's stable ordering (first-seen wins a tie).
func orderedCounts(entries []string) []countPair {
	counts := countEntries(entries)
	seen := make(map[string]bool, len(counts))
	order := make([]string, 0, len(counts))
	for _, entry := range entries {
		key := normalizeEntry(entry)
		if seen[key] {
			continue
		}
		seen[key] = true
		order = append(order, key)
	}
	pairs := make([]countPair, 0, len(order))
	for _, key := range order {
		pairs = append(pairs, countPair{key: key, count: counts[key]})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})
	return pairs
}

// orderEntries is order_entries(): 'freq' reorders by usage count.
// countSource must be the raw (pre-deduplication) list.
func orderEntries(entries []string, sortMode string, countSource []string) []string {
	if sortMode != "freq" {
		return entries
	}
	counts := countEntries(countSource)
	countOf := func(entry string) int {
		key := normalizeEntry(entry)
		if value, ok := counts[key]; ok {
			return value
		}
		return 1 // Python used counts.get(key, 1)
	}
	out := make([]string, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		return countOf(out[i]) > countOf(out[j])
	})
	return out
}

// rawEntries is raw_entries(): all history entries, without deduplication.
func rawEntries(cfg *Config, joinContinuations bool) (string, string, []string, error) {
	label, path := resolveHistory(cfg)
	if path == "" || !isFile(path) {
		return label, path, []string{}, nil
	}
	text, err := readTextFile(path, true)
	if err != nil {
		return label, path, nil, err
	}
	entries := entriesForSource(label, text, joinContinuations)
	out := []string{}
	for _, entry := range entries {
		if !isNoise(entry, cfg.Exclude) {
			out = append(out, entry)
		}
	}
	return label, path, out, nil
}

// loadHistory is load_history(): deduplicated and ordered.
func loadHistory(cfg *Config, joinContinuations bool) (string, string, []string, error) {
	label, path, raw, err := rawEntries(cfg, joinContinuations)
	if err != nil {
		return label, path, nil, err
	}
	entries := dedupEntries(raw)
	entries = orderEntries(entries, cfg.Sort, raw)
	if cfg.MaxItems > 0 && len(entries) > cfg.MaxItems {
		entries = entries[:cfg.MaxItems]
	}
	return label, path, entries, nil
}

// --- local cache (idea 5) ---------------------------------------------------

var cacheKeyCleanup = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// cachePath is cache_path().
func cachePath(cfg *Config, key string) string {
	safe := truncateChars(cacheKeyCleanup.ReplaceAllString(key, "_"), 80)
	safe = strings.Trim(safe, "_")
	if safe == "" {
		safe = "entry"
	}
	return filepath.Join(dataPath(cfg, "cache_dir"), safe+".txt")
}

// cacheGet is cache_get(): honours the TTL and the cache switch.
func cacheGet(cfg *Config, key string) string {
	if !cfg.Cache {
		return ""
	}
	path := cachePath(cfg, key)
	if !isFile(path) {
		return ""
	}
	ttl := cfg.CacheTTLHours * 3600
	if ttl > 0 {
		if modified, ok := fileModTime(path); ok {
			if nowSeconds()-modified > ttl {
				return ""
			}
		}
	}
	text, err := readTextFile(path, false)
	if err != nil {
		return ""
	}
	return text
}

// cachePut is cache_put(): best effort. A missing cache entry only costs a
// lookup, so a failed write must not interrupt the user.
func cachePut(cfg *Config, key string, body string) {
	if !cfg.Cache || body == "" {
		return
	}
	_ = atomicWrite(cachePath(cfg, key), body)
}
