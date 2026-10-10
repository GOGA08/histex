package main

// recipes.go - markdown recipes, the JSONL mirror and runnable scripts.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type recipe struct {
	title    string
	tags     []string
	commands []string
	scripts  []string
}

// scriptExtensions is SCRIPT_EXTENSIONS.
var scriptExtensions = map[string]string{
	"2": ".ps1",
	"3": ".bat",
	"4": ".sh",
	"5": ".cmd",
}

var slugCleanup = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// savePathWords are the reserved answers of the destination prompt.
var savePathWords = map[string]bool{
	"...":    true,
	"..":     true,
	".":      true,
	"cancel": true,
	"quit":   true,
	"exit":   true,
}

// resolveSaveDir validates the destination answer against the current base:
// parent moves, plain paths, cd commands and a one-shot fzf launch.
func resolveSaveDir(answer string, base string, pickDir func(string) (string, bool)) (string, bool) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == ".." {
		parent := filepath.Dir(base)
		if !isDir(parent) {
			return "", false
		}
		return parent, true
	}
	if trimmed == "." {
		if !isDir(base) {
			return "", false
		}
		return base, true
	}
	if trimmed == "" || savePathWords[trimmed] && trimmed != "..." {
		return "", false
	}
	if trimmed == "..." {
		if pickDir == nil {
			return "", false
		}
		picked, ok := pickDir(base)
		if !ok || !isDir(picked) {
			return "", false
		}
		return picked, true
	}
	fields := strings.Fields(trimmed)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "cd") {
		return validatedDir(strings.Join(fields[1:], " "), base)
	}
	return validatedDir(trimmed, base)
}

// validatedDir accepts an existing directory, or a path whose parent exists:
// the missing folder is created when the script is actually written, so a
// cancelled prompt never leaves an empty folder behind.
func validatedDir(word string, base string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(word), `"'`)
	if trimmed == "" || savePathWords[trimmed] {
		return "", false
	}
	expanded := expandUser(trimmed)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(base, expanded)
	}
	cleaned, err := filepath.Abs(expanded)
	if err != nil {
		return "", false
	}
	if canCreateDir(cleaned) {
		return cleaned, true
	}
	return "", false
}

// listChildDirs returns child directories of base as a flat sorted list, with a
// leading ".." only when a usable parent exists. No tree is ever printed.
func listChildDirs(base string, limit int) []string {
	children := []string{}
	parent := filepath.Dir(base)
	if parent != base && isDir(parent) {
		children = append(children, "..")
	}
	names, err := filepath.Glob(filepath.Join(base, "*"))
	if err != nil {
		return children
	}
	for _, name := range names {
		if limit > 0 && len(children) >= limit {
			break
		}
		if isDir(name) {
			children = append(children, filepath.Base(name))
		}
	}
	return children
}

// hasReservedName reports Windows-reserved base names and drive letters.
func hasReservedName(base string) bool {
	upper := strings.ToUpper(strings.TrimSuffix(base, filepath.Ext(base)))
	switch upper {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// validScriptFileName validates a script file name without creating a
// fallback. Unusable input returns ok=false so the save aborts instead of
// inventing a name.
func validScriptFileName(name string, extension string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(name), `"'`)
	base := filepath.Base(trimmed)
	if base == "" || base == "." || base == ".." {
		return "", false
	}
	if strings.ContainsAny(base, "<>:\"|?*\x00") {
		return "", false
	}
	if strings.HasSuffix(base, " ") || strings.HasSuffix(base, ".") {
		return "", false
	}
	if hasReservedName(base) {
		return "", false
	}
	lowered := strings.ToLower(extension)
	if lowered != "" && strings.HasSuffix(strings.ToLower(base), lowered) {
		return base, true
	}
	return base + extension, true
}

// askSaveDir is the one-line destination loop. The prompt always shows the
// current directory in brackets; cd/paths move, ".." goes up, "..." opens one
// fzf window, and blank input accepts the shown directory. Anything unusable
// repeats the single line; cancellation aborts the whole save.
func askSaveDir(label string, def string, pickDir func(string) (string, bool)) (string, bool) {
	current := strings.TrimSpace(def)
	if expanded := expandUser(current); expanded != "" {
		if resolved, err := filepath.Abs(expanded); err == nil {
			current = resolved
		}
	}
	for turns := 0; turns < 200; turns++ {
		answer, proceed := ask(fmt.Sprintf("%s [%s]> ", label, current), "")
		if !proceed {
			return "", false
		}
		raw := strings.TrimSpace(answer)
		if raw == "" {
			// ENTER accepts the folder on show; a brand new one is fine, the
			// write creates it, and a cancelled prompt then leaves nothing.
			if canCreateDir(current) {
				return current, true
			}
			return "", false
		}
		lowered := strings.ToLower(raw)
		if lowered == "cancel" || lowered == "quit" || lowered == "exit" || lowered == "n" || lowered == "no" {
			return "", false
		}
		next, ok := resolveSaveDir(raw, current, pickDir)
		if !ok {
			errLine("[x] not a directory: %s", raw)
			continue
		}
		current = next
	}
	errLine("[x] too many directory moves - the save is cancelled.")
	return "", false
}

// pickDirFzf opens one flat fzf list with the child directories and returns
// the chosen one. Cancel returns ok=false, which aborts the whole save.
func pickDirFzf(cfg *Config, base string) (string, bool) {
	if !isDir(base) {
		return "", false
	}
	status, _, selected := runFzf(listChildDirs(base, 1000), cfg, false, "save dir> ", nil)
	if status != "ok" || len(selected) == 0 {
		return "", false
	}
	choice := strings.TrimSpace(selected[0])
	if choice == "" || choice == "." {
		return base, true
	}
	if choice == ".." {
		parent := filepath.Dir(base)
		if !isDir(parent) {
			return "", false
		}
		return parent, true
	}
	candidate := filepath.Join(base, choice)
	if !isDir(candidate) {
		return "", false
	}
	return candidate, true
}

// parentDir returns the directory part of a file path.
func parentDir(path string) string {
	return filepath.Dir(path)
}

// canCreateDir reports whether a folder is there or can be created right next
// to an existing one. The creation itself happens on the first real write, so
// an abandoned prompt never leaves an empty folder behind.
func canCreateDir(path string) bool {
	if path == "" {
		return false
	}
	return isDir(path) || isDir(parentDir(path))
}

// samePath compares two file paths after normalization.
func samePath(first string, second string) bool {
	if first == "" || second == "" {
		return false
	}
	absoluteFirst, errFirst := filepath.Abs(first)
	absoluteSecond, errSecond := filepath.Abs(second)
	if errFirst != nil || errSecond != nil {
		return false
	}
	return normCase(absoluteFirst) == normCase(absoluteSecond)
}

// slugify is slugify().
func slugify(text string, fallback string) string {
	slug := strings.ToLower(strings.Trim(slugCleanup.ReplaceAllString(text, "-"), "-._"))
	slug = truncateChars(slug, 60)
	if slug == "" {
		return fallback
	}
	return slug
}

// scriptBody is script_body().
func scriptBody(commands []string, extension string) string {
	joined := strings.Join(commands, "\n")
	switch extension {
	case ".bat", ".cmd":
		return "@echo off\r\n" + strings.ReplaceAll(joined, "\n", "\r\n") + "\r\n"
	case ".sh":
		return "#!/usr/bin/env bash\nset -euo pipefail\n\n" + joined + "\n"
	case ".ps1":
		return "#Requires -Version 5.1\n$ErrorActionPreference = 'Stop'\n\n" + joined + "\n"
	}
	return joined + "\n"
}

// writeScriptTo writes ready text to an exact path with one atomic rename. A
// half-written file can never stay behind: either the rename succeeds or the
// old file keeps its bytes and the temporary copy is removed.
func writeScriptTo(path string, payload []byte) error {
	if err := ensureDir(parentDir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parentDir(path), ".histex-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o666); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// atomicFileSize returns the byte size, or -1 when there is no file.
func atomicFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return -1
	}
	return info.Size()
}

// truncateFileTo removes bytes appended after size, or deletes a file that did
// not exist. It keeps previously saved recipes untouched when a later step in
// the same save fails.
func truncateFileTo(path string, size int64) error {
	if size < 0 {
		if !isFile(path) {
			return nil
		}
		return os.Remove(path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return err
	}
	if info.Size() <= size {
		return nil
	}
	return os.Truncate(path, size)
}

// removeScriptTarget deletes a script file created in a save that later fails.
// Empty and missing paths are ignored.
func removeScriptTarget(path string) {
	if path == "" || !isFile(path) {
		return
	}
	os.Remove(path)
}

// resolveScriptTarget turns a file answer into a script destination inside dir.
// ok=false aborts the save: the answer was cancelled, unsafe, unusable, or the
// user declined the overwrite question.
func resolveScriptTarget(answer string, dir string, extension string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(answer), `"'`)
	if trimmed == "" {
		return "", false
	}
	lowered := strings.ToLower(trimmed)
	if lowered == "cancel" || lowered == "quit" || lowered == "exit" || lowered == "n" || lowered == "no" {
		return "", false
	}
	if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(dir, trimmed)
	}
	parent := parentDir(trimmed)
	if !canCreateDir(parent) {
		return "", false
	}
	base, ok := validScriptFileName(filepath.Base(trimmed), extension)
	if !ok {
		return "", false
	}
	target := filepath.Join(parent, base)
	// Ask only when the file is really there: a fresh name must never produce
	// a "File exists" question.
	if isFile(target) {
		if answer, proceed := ask(fmt.Sprintf("File exists. Overwrite %s? [y/N]: ", target), ""); !proceed || !isAffirmative(answer) {
			return "", false
		}
	}
	return target, true
}

// isAffirmative accepts only explicit yes answers. Everything else,
// including blank input, cancellation and "no", means no.
func isAffirmative(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "1", "true", "k", "ki", "ho", "diax":
		return true
	}
	return false
}

// scriptPayload builds the exact bytes stored for one extension.
func scriptPayload(commands []string, extension string) ([]byte, bool) {
	body := scriptBody(commands, extension)
	payload := make([]byte, 0, len(body)+3)
	if extension != ".bat" && extension != ".cmd" && extension != ".sh" {
		payload = append(payload, 0xEF, 0xBB, 0xBF)
	}
	return append(payload, body...), true
}

// recipeLabel is the one line the recipe library shows: title, tags, command
// count, and where the runnable files live - so the list can be searched by
// folder as well.
func recipeLabel(item recipe) string {
	label := item.title
	if len(item.tags) > 0 {
		label += fmt.Sprintf("   [%s]", strings.Join(item.tags, ", "))
	}
	label += fmt.Sprintf("   (%d cmd)", len(item.commands))
	if len(item.scripts) > 0 {
		label += "   -> " + strings.Join(item.scripts, scriptsSeparator)
	}
	return label
}

// storedScripts returns the recorded script paths of an already saved command
// set, so the duplicate question can say where the file lives.
func storedScripts(cfg *Config, commands []string) []string {
	joined := strings.TrimSpace(strings.Join(commands, "\n"))
	for _, item := range parseRecipes(cfg, nil) {
		if strings.TrimSpace(strings.Join(item.commands, "\n")) == joined {
			return item.scripts
		}
	}
	return nil
}

// recipesAppend is recipes_append(): appends one recipe block. The script paths
// travel with the block as a meta comment, so the markdown history records
// where the runnable file actually lives on this machine.
func recipesAppend(cfg *Config, title string, commands []string, tags []string, scripts []string) (string, error) {
	path := dataPath(cfg, "recipes")
	stamp := time.Now().Format("2006-01-02 15:04")
	heading := fmt.Sprintf("### Date: %s - %s", stamp, title)
	if len(tags) > 0 {
		heading += fmt.Sprintf("   <!-- tags: %s -->", strings.Join(tags, ", "))
	}
	block := heading + "\n"
	if len(scripts) > 0 {
		block += fmt.Sprintf("\n<!-- scripts: %s -->\n", strings.Join(scripts, scriptsSeparator))
	}
	block += fmt.Sprintf("\n```bash\n%s\n```\n\n", strings.Join(commands, "\n"))
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return path, err
	}
	// A UTF-8 BOM stops PowerShell 5.1's Get-Content from garbling non-ASCII.
	return path, appendTextFile(path, block, !isFile(path), true)
}

// jsonlAppend is jsonl_append(): the machine readable mirror.
func jsonlAppend(cfg *Config, title string, commands []string, tags []string, scripts []string) string {
	path := dataPath(cfg, "jsonl")
	if path == "" {
		return ""
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return ""
	}
	record := jobject{
		{"ts", time.Now().Format("2006-01-02 15:04:05")},
		{"title", title},
		{"tags", emptyList(tags)},
		{"commands", emptyList(commands)},
		{"scripts", emptyList(scripts)},
	}
	if err := appendTextFile(path, pyJSON(record, "", 0)+"\n", false, true); err != nil {
		return ""
	}
	return path
}

// emptyList mirrors `tags or []`: a Python empty list serialises as [].
func emptyList(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

// recipeExists is recipe_exists(): is this exact command set already stored?
func recipeExists(cfg *Config, commands []string) bool {
	path := dataPath(cfg, "recipes")
	if !isFile(path) {
		return false
	}
	text, err := readTextFile(path, true)
	if err != nil {
		return false
	}
	return strings.Contains(text, strings.TrimSpace(strings.Join(commands, "\n")))
}

func firstLine(text string) string {
	lines := pySplitLines(text)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// stdinReader is shared: a fresh bufio.Reader per prompt would drop the rest
// of an already buffered stdin.
var stdinReader = bufio.NewReader(os.Stdin)

// ask is ask(): input() that survives EOF/CTRL-C. ok=false means cancelled,
// and an empty answer falls back to the default.
func ask(prompt string, def string) (string, bool) {
	outRaw(prompt)
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		outLine("")
		return "", false
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return def, true
	}
	return answer, true
}

// askTitle is ask_title(): suggests a title derived from the command.
func askTitle(commands []string) (string, bool) {
	suggestion := "recipe"
	if len(commands) > 0 {
		if fields := strings.Fields(commands[0]); len(fields) > 0 {
			if name := normalizeToken(fields[0]); name != "" {
				suggestion = name
			}
		}
	}
	return ask(fmt.Sprintf("Title [%s]: ", suggestion), suggestion)
}

var choiceSplit = regexp.MustCompile(`[\s,]+`)

// saveRecipeFlow is save_recipe_flow(): the CTRL-T action. Every question is
// asked first; the three disk writes happen only afterwards, so a cancelled or
// unusable answer leaves the recipe files exactly as they were. ok=false means
// the user stopped the flow, not an error: nothing is written in that case.
func saveRecipeFlow(commands []string, cfg *Config) bool {
	cleaned := []string{}
	for _, command := range commands {
		if strings.TrimSpace(command) != "" {
			cleaned = append(cleaned, command)
		}
	}
	if len(cleaned) == 0 {
		errLine("[x] nothing to save: the selection is empty.")
		return false
	}
	if recipeExists(cfg, cleaned) {
		detail := ""
		if known := storedScripts(cfg, cleaned); len(known) > 0 {
			detail = fmt.Sprintf(" (script: %s)", strings.Join(known, scriptsSeparator))
		}
		answer, proceed := ask(fmt.Sprintf(
			"This exact command set is already saved%s. Add it again? [y/N]: ", detail), "")
		if !proceed {
			outLine("[i] cancelled - nothing saved.")
			return false
		}
		if !isAffirmative(answer) {
			outLine("[i] kept the existing recipe - nothing saved.")
			return false
		}
	}
	title, ok := askTitle(cleaned)
	if !ok || strings.TrimSpace(title) == "" {
		outLine("[i] cancelled - nothing saved.")
		return false
	}
	title = strings.TrimSpace(title)
	tagInput, ok := ask("Tags (comma separated, optional): ", "")
	if !ok {
		outLine("[i] cancelled - nothing saved.")
		return false
	}
	tags := []string{}
	for _, tag := range strings.Split(tagInput, ",") {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	outLine("Format:  [1] markdown only   [2] +PowerShell .ps1   [3] +batch .bat   " +
		"[4] +bash .sh   [5] +cmd .cmd   (combine, e.g. 1,2)")
	choice := "1"
	if value, chosen := ask("Choice [1]: ", "1"); chosen {
		choice = value
	} else {
		outLine("[i] cancelled - nothing saved.")
		return false
	}
	picked := []string{}
	seenExt := map[string]bool{}
	for _, key := range choiceSplit.Split(choice, -1) {
		extension := scriptExtensions[strings.TrimSpace(key)]
		if extension == "" || seenExt[extension] {
			continue
		}
		seenExt[extension] = true
		picked = append(picked, extension)
	}
	type scriptPlan struct {
		extension string
		target    string
		payload   []byte
	}
	plans := []scriptPlan{}
	defaultDir := dataPath(cfg, "scripts_dir")
	for _, extension := range picked {
		if (extension == ".bat" || extension == ".cmd") &&
			!isASCII(strings.Join(cleaned, "\n")) {
			errLine("[!] batch files do not handle non-ASCII text well - " +
				"consider .ps1 instead.")
		}
		scriptName := slugify(title, "recipe") + extension
		dir, ok := askSaveDir(fmt.Sprintf("save %s to", extension), defaultDir, func(base string) (string, bool) {
			return pickDirFzf(cfg, base)
		})
		if !ok {
			outLine("[i] cancelled - nothing saved.")
			return false
		}
		answer, proceed := ask(fmt.Sprintf("Script file [%s]> ", scriptName), scriptName)
		if !proceed {
			outLine("[i] cancelled - nothing saved.")
			return false
		}
		target, ok := resolveScriptTarget(answer, dir, extension)
		if !ok {
			outLine("[i] cancelled - nothing saved.")
			return false
		}
		payload, _ := scriptPayload(cleaned, extension)
		plans = append(plans, scriptPlan{extension, target, payload})
		defaultDir = parentDir(target)
	}

	recipesPath := dataPath(cfg, "recipes")
	jsonlPath := dataPath(cfg, "jsonl")
	markdownSize := atomicFileSize(recipesPath)
	jsonlSize := atomicFileSize(jsonlPath)
	// Whatever was already on disk is what a failed save has to restore: the
	// bytes of every script this save overwrites, keyed by target.
	previous := map[string][]byte{}
	wroteScripts := []string{}
	fail := func(format string, args ...any) bool {
		for index := len(wroteScripts) - 1; index >= 0; index-- {
			target := wroteScripts[index]
			if raw, had := previous[target]; had {
				writeScriptTo(target, raw) // put the old script back
				continue
			}
			removeScriptTarget(target)
		}
		truncateFileTo(recipesPath, markdownSize)
		truncateFileTo(jsonlPath, jsonlSize)
		errLine(format, args...)
		return false
	}
	scripts := []string{}
	for _, plan := range plans {
		if raw, err := os.ReadFile(plan.target); err == nil {
			previous[plan.target] = raw
		}
		if err := writeScriptTo(plan.target, plan.payload); err != nil {
			return fail("[x] could not write script: %s", err)
		}
		wroteScripts = append(wroteScripts, plan.target)
		scripts = append(scripts, plan.target)
		outLine("[ok] script: %s", plan.target)
	}
	if _, err := recipesAppend(cfg, title, cleaned, tags, scripts); err != nil {
		return fail("[x] could not write the recipe file: %s", err)
	}
	jsonlAppend(cfg, title, cleaned, tags, scripts)
	outLine("[ok] recipe: %s", recipesPath)
	return true
}

var (
	tagsMarker    = regexp.MustCompile(`<!--\s*tags:\s*([^>]*?)-->`)
	scriptsMarker = regexp.MustCompile(`<!--\s*scripts:\s*([^>]*?)-->`)
	datePrefix    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\s+\d{2}:\d{2})?\s*-\s*`)
)

// scriptsSeparator keeps generated script paths readable inside the markdown
// meta comment; it is not a legal character in a Windows path.
const scriptsSeparator = " | "

// parseRecipes is parse_recipes(): markdown -> [(title, tags, commands)].
// A nil text means "read the recipe file".

func parseRecipes(cfg *Config, text *string) []recipe {
	body := ""
	if text != nil {
		body = *text
	} else {
		path := dataPath(cfg, "recipes")
		if !isFile(path) {
			return []recipe{}
		}
		loaded, err := readTextFile(path, true)
		if err != nil {
			return []recipe{}
		}
		body = loaded
	}
	recipes := []recipe{}
	title := ""
	haveTitle := false
	tags := []string{}
	scripts := []string{}
	commands := []string{}
	inCode := false
	for _, line := range pySplitLines(body) {
		if strings.HasPrefix(line, "### ") {
			if haveTitle {
				recipes = append(recipes, recipe{title, tags, commands, scripts})
			}
			heading := strings.TrimSpace(line[4:])
			tags = []string{}
			scripts = []string{}
			if location := tagsMarker.FindStringSubmatchIndex(heading); location != nil {
				inner := heading[location[2]:location[3]]
				for _, tag := range strings.Split(inner, ",") {
					if trimmed := strings.TrimSpace(tag); trimmed != "" {
						tags = append(tags, trimmed)
					}
				}
				heading = strings.TrimSpace(heading[:location[0]])
			}
			if strings.HasPrefix(strings.ToLower(heading), "date: ") {
				heading = strings.TrimSpace(heading[6:])
			}
			// Drop the timestamp so the browser shows only the real title.
			heading = strings.TrimSpace(datePrefix.ReplaceAllString(heading, ""))
			title, commands, inCode = heading, []string{}, false
			haveTitle = true
			continue
		}
		if !haveTitle {
			continue
		}
		if location := scriptsMarker.FindStringSubmatch(line); location != nil {
			for _, script := range strings.Split(location[1], scriptsSeparator) {
				if trimmed := strings.TrimSpace(script); trimmed != "" {
					scripts = append(scripts, trimmed)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			commands = append(commands, line)
		}
	}
	if haveTitle {
		recipes = append(recipes, recipe{title, tags, commands, scripts})
	}
	return recipes
}

// recipesMode is recipes_mode(): --browse searches the saved recipes.
func recipesMode(cfg *Config) int {
	recipes := parseRecipes(cfg, nil)
	if len(recipes) == 0 {
		errLine("[i] no recipes yet - save one with CTRL-T first.")
		return 0
	}
	lookup := map[string]int{}
	display := []string{}
	mapping := jobject{}
	for index, item := range recipes {
		line := recipeLabel(item)
		for {
			if _, taken := lookup[line]; !taken {
				break
			}
			line += " "
		}
		lookup[line] = index
		display = append(display, line)
		mapping = append(mapping, jpair{line, item.commands})
	}

	status, selected := "cancel", []string{}
	handle, err := os.CreateTemp("", "histex-*.histex-preview.json")
	if err == nil {
		mapPath := handle.Name()
		handle.WriteString(pyJSON(mapping, "", 0))
		handle.Close()
		defer os.Remove(mapPath)
		previous, hadPrevious := os.LookupEnv("HISTEX_PREVIEW_MAP")
		os.Setenv("HISTEX_PREVIEW_MAP", mapPath)
		defer func() {
			if hadPrevious {
				os.Setenv("HISTEX_PREVIEW_MAP", previous)
			} else {
				os.Unsetenv("HISTEX_PREVIEW_MAP")
			}
		}()

		// The reload is a fresh process, so the prepared recipe list travels in
		// a file (HISTEX_RECIPES_LIST) - the same trick as the preview map - so
		// the reload shows the exact same titles, duplicate disambiguation
		// included.
		if listHandle, listErr := os.CreateTemp("", "histex-*.histex-recipes.txt"); listErr == nil {
			listPath := listHandle.Name()
			listHandle.WriteString(listText(display))
			listHandle.Close()
			os.Setenv("HISTEX_RECIPES_LIST", listPath)
			defer os.Remove(listPath)
			defer os.Unsetenv("HISTEX_RECIPES_LIST")
		}

		// Own prompt + visible preview: this is the recipe library,
		// not the history picker - it should never look like plain fzf.
		local := *cfg
		local.Preview = true
		status, _, selected = runFzf(display, &local, true, "recipes> ", []string{"--print-list"})
	}
	if status != "ok" || len(selected) == 0 {
		return 0
	}
	index, found := lookup[selected[0]]
	if !found {
		return 0
	}
	item := recipes[index]
	if stdoutIsPayload {
		// The wrapper pastes stdout into the prompt, so in --pick / --json
		// mode the recipe's commands are the payload; the pretty block below
		// is human text and stays on stderr via outLine.
		emitSelection(item.commands, payloadAsJSON)
	}
	outLine("")
	if len(item.tags) > 0 {
		outLine("### %s   [%s]", item.title, strings.Join(item.tags, ", "))
	} else {
		outLine("### %s", item.title)
	}
	for _, command := range item.commands {
		outLine("%s", "    "+command)
	}
	outLine("")
	doClipboard(item.commands)
	return 0
}
