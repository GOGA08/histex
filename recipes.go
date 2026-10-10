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
}

// scriptExtensions is SCRIPT_EXTENSIONS.
var scriptExtensions = map[string]string{
	"2": ".ps1",
	"3": ".bat",
	"4": ".sh",
	"5": ".cmd",
}

var slugCleanup = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

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

// writeScript is write_script(). cmd.exe chokes on a UTF-8 BOM; PowerShell
// needs one for non-ASCII text.
func writeScript(cfg *Config, title string, commands []string, extension string) (string, error) {
	directory := dataPath(cfg, "scripts_dir")
	if err := ensureDir(directory); err != nil {
		return "", err
	}
	path := filepath.Join(directory, slugify(title, "recipe")+extension)
	bom := extension != ".bat" && extension != ".cmd" && extension != ".sh"
	return path, writeTextFile(path, scriptBody(commands, extension), bom, false)
}

// recipesAppend is recipes_append(): appends one recipe block.
func recipesAppend(cfg *Config, title string, commands []string, tags []string) (string, error) {
	path := dataPath(cfg, "recipes")
	stamp := time.Now().Format("2006-01-02 15:04")
	heading := fmt.Sprintf("### Date: %s - %s", stamp, title)
	if len(tags) > 0 {
		heading += fmt.Sprintf("   <!-- tags: %s -->", strings.Join(tags, ", "))
	}
	block := fmt.Sprintf("%s\n\n```bash\n%s\n```\n\n", heading, strings.Join(commands, "\n"))
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

// saveRecipeFlow is save_recipe_flow(): the CTRL-T action.
func saveRecipeFlow(commands []string, cfg *Config) bool {
	outLine("")
	outLine("Marked commands (%d):", len(commands))
	for _, command := range commands {
		suffix := ""
		if strings.Contains(command, "\n") {
			suffix = " ..."
		}
		outLine("%s", "   "+firstLine(command)+suffix)
	}
	outLine("")
	for _, command := range commands {
		if reason, found := dangerReason(command, cfg); found {
			errLine("[!] destructive pattern '%s' in: %s", reason, firstLine(command))
		}
	}
	if recipeExists(cfg, commands) {
		answer, ok := ask(
			"This exact command set is already saved. Add it again? [y/N]: ", "")
		lowered := strings.ToLower(answer)
		if !ok || (lowered != "y" && lowered != "yes") {
			outLine("[i] nothing saved.")
			return false
		}
	}
	title, ok := askTitle(commands)
	if !ok {
		outLine("[i] cancelled.")
		return false
	}
	tagInput, _ := ask("Tags (comma separated, optional): ", "")
	tags := []string{}
	for _, tag := range strings.Split(tagInput, ",") {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	outLine("Format:  [1] markdown   [2] +PowerShell .ps1   [3] +batch .bat   " +
		"[4] +bash .sh   (combine, e.g. 1,2)")
	choice := "1"
	if value, chosen := ask("Choice [1]: ", "1"); chosen {
		choice = value
	}
	scripts := []string{}
	for _, key := range choiceSplit.Split(choice, -1) {
		extension := scriptExtensions[key]
		if extension == "" {
			continue
		}
		if (extension == ".bat" || extension == ".cmd") &&
			!isASCII(strings.Join(commands, "\n")) {
			errLine("[!] batch files do not handle non-ASCII text well - " +
				"consider .ps1 instead.")
		}
		path, err := writeScript(cfg, title, commands, extension)
		if err != nil {
			errLine("[x] could not write script: %s", err)
			continue
		}
		scripts = append(scripts, path)
	}
	for _, path := range scripts {
		outLine("[ok] script: %s", path)
	}
	path, err := recipesAppend(cfg, title, commands, tags)
	if err != nil {
		errLine("[x] could not write the recipe file: %s", err)
		return false
	}
	jsonlAppend(cfg, title, commands, tags, scripts)
	outLine("[ok] recipe: %s", path)
	return true
}

var (
	tagsMarker = regexp.MustCompile(`<!--\s*tags:\s*([^>]*?)-->`)
	datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\s+\d{2}:\d{2})?\s*-\s*`)
)

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
	commands := []string{}
	inCode := false
	for _, line := range pySplitLines(body) {
		if strings.HasPrefix(line, "### ") {
			if haveTitle {
				recipes = append(recipes, recipe{title, tags, commands})
			}
			heading := strings.TrimSpace(line[4:])
			tags = []string{}
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
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			commands = append(commands, line)
		}
	}
	if haveTitle {
		recipes = append(recipes, recipe{title, tags, commands})
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
		label := item.title
		if len(item.tags) > 0 {
			label += fmt.Sprintf("   [%s]", strings.Join(item.tags, ", "))
		}
		line := fmt.Sprintf("%s   (%d cmd)", label, len(item.commands))
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
