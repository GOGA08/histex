package main

// explain.go - the explanation chain, the query planner and the previews.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// --- online lookup: cheat.sh ------------------------------------------------

const (
	cheatURL     = "https://cheat.sh/"
	cheatUA      = "curl/8.5.0" // mandatory: otherwise cheat.sh answers with HTML
	cheatTimeout = 10
)

// httpStatusError carries the status code of a failed HTTP response.
type httpStatusError struct {
	code int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d", e.code)
}

// cheatMiss is cheat_miss(): cheat.sh answers 200 even for a miss.
func cheatMiss(body string) bool {
	lowered := strings.ToLower(body)
	return strings.Contains(lowered, "404 not found") ||
		strings.Contains(lowered, "unknown topic") ||
		strings.Contains(lowered, "unknown cheat sheet")
}

// fetchCheat is fetch_cheat(): the sheet text, or "" when nothing was found.
func fetchCheat(query string) (string, error) {
	target := cheatURL + pyURLQuote(query) + "?T"
	client := &http.Client{Timeout: cheatTimeout * time.Second}
	request, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", cheatUA)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode >= 400 {
		return "", httpStatusError{code: response.StatusCode}
	}
	body := decodeText(raw, false)
	if cheatMiss(body) {
		return "", nil
	}
	return body, nil
}

// --- query planning ---------------------------------------------------------

// splitPipeline is split_pipeline(): 'a | b; c && d' -> single commands.
func splitPipeline(command string) []string {
	parts := []string{}
	var buffer strings.Builder
	quote := rune(0)
	runes := []rune(command)
	index := 0
	for index < len(runes) {
		char := runes[index]
		if quote != 0 {
			buffer.WriteRune(char)
			if char == quote {
				quote = 0
			}
			index++
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			buffer.WriteRune(char)
			index++
			continue
		}
		pair := ""
		if index+1 < len(runes) {
			pair = string(runes[index : index+2])
		}
		if pair == "&&" || pair == "||" {
			parts = append(parts, buffer.String())
			buffer.Reset()
			index += 2
			continue
		}
		if char == '|' || char == ';' {
			parts = append(parts, buffer.String())
			buffer.Reset()
			index++
			continue
		}
		buffer.WriteRune(char)
		index++
	}
	parts = append(parts, buffer.String())
	out := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}

// flagsPattern is the RE2 equivalent of r"(?<!\S)--?[A-Za-z][A-Za-z0-9-]*":
// a flag is only a flag when it starts the string or follows whitespace.
var flagsPattern = regexp.MustCompile(`(?:^|\s)(--?[A-Za-z][A-Za-z0-9-]*)`)

// flagsIn is flags_in(): the -x / --long flags found in the command.
func flagsIn(command string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, match := range flagsPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		out = append(out, match[1])
	}
	sort.Strings(out)
	return out
}

// candidates is candidates(): the ordered query candidates for a lookup.
func candidates(command string, cfg *Config, resolve bool) []string {
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return []string{}
	}
	first := normalizeToken(tokens[0])
	raw := []string{}
	if len(tokens) >= 2 && !strings.HasPrefix(tokens[1], "-") {
		raw = append(raw, first+" "+normalizeToken(tokens[1]))
	}
	raw = append(raw, first)
	if resolve {
		if alias, ok := resolveAlias(first, cfg); ok {
			raw = append(raw, normalizeToken(alias))
		}
	}
	queries := []string{}
	for _, item := range raw {
		item = strings.ToLower(item)
		if item == "" {
			continue
		}
		duplicate := false
		for _, existing := range queries {
			if existing == item {
				duplicate = true
				break
			}
		}
		if !duplicate {
			queries = append(queries, item)
		}
	}
	return queries
}

// explainTail is _explain_tail(): the flags used, in 'full' detail only.
func explainTail(command string, cfg *Config) {
	if cfg.Detail != "full" {
		return
	}
	flags := flagsIn(command)
	if len(flags) > 0 {
		outLine("")
		outLine("flags used: %s", strings.Join(flags, " "))
	}
}

// --- local tldr pages (offline source, idea 16) -----------------------------

// tldrPath is tldr_path().
func tldrPath() string {
	return lookWhich("tldr")
}

// runTldr is _run_tldr(): (text, hint) for one query.
func runTldr(exe string, query string) (string, string) {
	hint := ""
	variants := [][]string{
		{"-q", query},
		{"-q", "--platform", "windows", query},
	}
	for _, args := range variants {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, exe, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		if err != nil {
			if _, isExit := err.(*exec.ExitError); !isExit || timedOut {
				return "", ""
			}
		}
		out := strings.TrimSpace(stdout.String())
		errorText := stderr.String()
		if strings.Contains(strings.ToLower(errorText), "cache not found") {
			hint = "tldr page cache is missing - run:  tldr --update"
			continue
		}
		if stringWidth(out) > 20 {
			return out, ""
		}
	}
	return "", hint
}

// tldrLookup is tldr_lookup(): the cached mirror keeps --preview and
// --offline working without starting tldr again.
func tldrLookup(command string, cfg *Config) string {
	if !cfg.Tldr {
		return ""
	}
	exe := tldrPath()
	if exe == "" {
		return ""
	}
	hint := ""
	for _, query := range candidates(command, cfg, false) {
		text, queryHint := runTldr(exe, query)
		hint = queryHint // Python reassigned it on every pass
		if text != "" {
			cachePut(cfg, "tldr:"+query, text)
			return text
		}
	}
	if hint != "" {
		errLine("%s", "[!] "+hint)
	}
	return ""
}

// updateTldr is update_tldr(): --update-tldr refreshes the local pages.
func updateTldr() int {
	exe := tldrPath()
	if exe == "" {
		errLine("[x] tldr is not installed.")
		errLine("    Install it with:")
		errLine("%s", tldrInstallHint())
		return 1
	}
	errLine("[..] tldr --update")
	cmd := exec.Command(exe, "--update")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			errLine("[x] tldr --update failed (exit code %s)",
				itoa(exitErr.ExitCode()))
			return 1
		}
		errLine("[x] could not run tldr: %s", err)
		return 1
	}
	outLine("[ok] local tldr page cache updated.")
	return 0
}

// cheatLookup is cheat_lookup(): the cache is checked before each request.
func cheatLookup(queries []string, cfg *Config) string {
	for index, query := range queries {
		if cached := cacheGet(cfg, query); cached != "" {
			return cached
		}
		errLine("[..] cheat.sh: %s", query)
		body, err := fetchCheat(query)
		if err != nil {
			var statusErr httpStatusError
			if errors.As(err, &statusErr) {
				if statusErr.code == 429 {
					errLine("[!] cheat.sh: 429 (rate limited) - try again " +
						"later; cached answers still work offline.")
				} else {
					errLine("[!] cheat.sh: HTTP %d", statusErr.code)
				}
			} else {
				errLine("[!] network error: %s (offline?)", err)
			}
			return ""
		}
		if body != "" {
			cachePut(cfg, query, body)
			return body
		}
		if index+1 < len(queries) {
			errLine("     no entry for '%s' - falling back to '%s'",
				query, queries[index+1])
		}
	}
	return ""
}

// previewText is preview_text(): must stay instant, so it only reads cache.
func previewText(command string, cfg *Config) string {
	segments := []string{}
	for _, segment := range splitPipeline(command) {
		segments = append(segments, strings.TrimSpace(segment))
	}
	head := strings.Join(segments, "\n\n")
	if len(segments) > 1 {
		head += fmt.Sprintf("\n\n(%d commands joined by | ; && ||)", len(segments))
	}
	for _, query := range candidates(command, cfg, false) {
		for _, key := range []string{"tldr:" + query, query} {
			if cached := cacheGet(cfg, key); cached != "" {
				return fmt.Sprintf("%s\n\n%s\n\n%s", head,
					strings.Repeat("-", 24), strings.TrimSpace(cached))
			}
		}
	}
	return fmt.Sprintf("%s\n\n%s\n\nno cached explanation yet - "+
		"press ENTER to explain", head, strings.Repeat("-", 24))
}

// recipesPreview is recipes_preview(): the recipe's commands, via the
// HISTEX_PREVIEW_MAP file that --browse wrote for the fzf preview calls.
func recipesPreview(displayLine string) string {
	mapping := map[string][]string{}
	if path := os.Getenv("HISTEX_PREVIEW_MAP"); path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var loaded map[string][]string
			if json.Unmarshal(raw, &loaded) == nil {
				mapping = loaded
			}
		}
	}
	commands, ok := mapping[displayLine]
	if !ok {
		// fzf may trim trailing padding spaces from the {} argument.
		commands = mapping[strings.TrimSpace(displayLine)]
	}
	lines := []string{strings.TrimSpace(displayLine), "",
		strings.Repeat("-", 24), ""}
	if len(commands) > 0 {
		lines = append(lines, commands...)
	} else {
		lines = append(lines, "(no commands stored)")
	}
	outLine("%s", strings.Join(lines, "\n"))
	return "recipe-preview"
}

// explain is explain(): prints an explanation for one command.
func explain(command string, cfg *Config, preview bool) string {
	if preview {
		outLine("%s", previewText(command, cfg))
		return "preview"
	}

	segments := splitPipeline(command)
	if len(segments) > 1 {
		for position, segment := range segments {
			outLine("")
			outLine("--- [%d/%d] %s", position+1, len(segments), segment)
			explain(segment, cfg, preview)
		}
		return "pipeline"
	}

	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return "none"
	}

	queries := candidates(command, cfg, true)
	definition, _ := resolveAlias(normalizeToken(tokens[0]), cfg)
	if warning := aliasWarning(tokens[0], definition); warning != "" {
		errLine("%s", "[!] "+warning)
	}

	allowNetwork := cfg.Network
	if secret, found := isSecret(command, cfg); found {
		errLine("[!] looks like a secret ('%s') - skipping the network.", secret)
		allowNetwork = false
	}

	localText, rich := localExplain(command, cfg)

	order := cfg.ExplainOrder
	if len(order) == 0 {
		order = []string{"cache", "local", "tldr", "cheat"}
	}
	for _, source := range order {
		text := ""
		switch source {
		case "cache":
			if len(queries) > 0 {
				text = cacheGet(cfg, queries[0])
			}
		case "local":
			if rich {
				text = localText
			}
		case "tldr":
			text = tldrLookup(command, cfg)
		case "cheat":
			if allowNetwork {
				text = cheatLookup(queries, cfg)
			}
		}
		if text != "" {
			outLine("")
			outLine("%s", strings.TrimSpace(text))
			explainTail(command, cfg)
			return source
		}
	}

	// Thin PowerShell help, if that is all we managed to find.
	if localText != "" {
		outLine("")
		outLine("%s", localText)
		explainTail(command, cfg)
		return "local(thin)"
	}

	if !allowNetwork {
		errLine("[i] nothing local for: %s (run once without --offline, and "+
			"make sure `tldr --update` has run)", command)
	} else {
		errLine("[i] nothing found for: %s", command)
	}
	return "none"
}
