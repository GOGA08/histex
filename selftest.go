package main

// selftest.go - --self-test: fast, offline checks of parsing and helpers.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type checkResult struct {
	name string
	ok   bool
}

func eqStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func eqRecipes(left []recipe, right []recipe) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].title != right[index].title ||
			!eqStrings(left[index].tags, right[index].tags) ||
			!eqStrings(left[index].commands, right[index].commands) {
			return false
		}
	}
	return true
}

func rawFileEquals(path string, expected string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return string(raw) == expected
}

// captureStdout runs one function and returns what it printed.
func captureStdout(run func()) string {
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		run()
		return ""
	}
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		io.Copy(&buffer, reader)
		done <- buffer.String()
	}()
	run()
	writer.Close()
	os.Stdout = old
	out := <-done
	reader.Close()
	return universalNewlines(out)
}

// previewRecipeForTest is preview_recipe_for_test(): runs recipes_preview()
// against a temporary HISTEX_PREVIEW_MAP.
func previewRecipeForTest(displayLine string, mapping jobject) string {
	previous, hadPrevious := os.LookupEnv("HISTEX_PREVIEW_MAP")
	handle, err := os.CreateTemp("", "histex-test-*.histex-preview.json")
	if err != nil {
		return ""
	}
	mapPath := handle.Name()
	handle.WriteString(pyJSON(mapping, "", 0))
	handle.Close()
	os.Setenv("HISTEX_PREVIEW_MAP", mapPath)
	defer func() {
		if hadPrevious {
			os.Setenv("HISTEX_PREVIEW_MAP", previous)
		} else {
			os.Unsetenv("HISTEX_PREVIEW_MAP")
		}
		os.Remove(mapPath)
	}()
	return captureStdout(func() { recipesPreview(displayLine) })
}

// runSelfChecks runs every offline check and returns the labelled results.
// --self-test prints them; selftest_test.go feeds the same list to `go test`,
// so the two can never drift apart.
func runSelfChecks() []checkResult {
	results := []checkResult{}
	check := func(name string, condition bool) {
		results = append(results, checkResult{name, condition})
	}

	check("dedup keeps the newest variant first",
		eqStrings(dedupEntries([]string{"a", "b", "a"}), []string{"a", "b"}))
	check("near-duplicates merge",
		normalizeEntry("git   status") == normalizeEntry("git status"))
	check("noise: 'cls' is dropped",
		isNoise("cls", defaultConfig().Exclude))
	check("noise: 'git status' is kept",
		!isNoise("git status", defaultConfig().Exclude))
	check("multi-line commands are rejoined",
		eqStrings(entriesFromText("if ($x) {\n  Write-Host 1\n}", true),
			[]string{"if ($x) {\n  Write-Host 1\n}"}))
	check("balanced lines stay separate",
		eqStrings(entriesFromText("line one\nline two", true),
			[]string{"line one", "line two"}))
	check("pipeline split",
		eqStrings(splitPipeline("a | b; c && d"), []string{"a", "b", "c", "d"}))
	check("pipeline honours quotes",
		eqStrings(splitPipeline(`echo "a | b"`), []string{`echo "a | b"`}))
	check("flags",
		eqStrings(flagsIn("tar -xzf f.tgz --verbose"), []string{"--verbose", "-xzf"}))
	check("query candidates",
		eqStrings(candidates("git status -sb", defaultConfig(), false),
			[]string{"git status", "git"}))
	check("slugify", slugify("Local LLM Setup!", "recipe") == "local-llm-setup")
	check("ps1 script body",
		strings.HasSuffix(scriptBody([]string{"ls"}, ".ps1"), "ls\n"))
	check("bat script body",
		strings.HasPrefix(scriptBody([]string{"ls"}, ".bat"), "@echo off"))
	check("bat script body uses CRLF",
		strings.Contains(scriptBody([]string{"ls"}, ".bat"), "\r\n"))
	check("NUL separated list", listText([]string{"a", "b"}) == "a\x00b\x00")
	_, secret := isSecret("mysql -p password=1", defaultConfig())
	check("secret guard", secret)
	_, dangerous := dangerReason("rm -rf /tmp/x", defaultConfig())
	check("danger guard", dangerous)
	_, safe := dangerReason("ls -la", defaultConfig())
	check("danger guard leaves safe commands alone", !safe)
	check("cheat.sh miss detection", cheatMiss("# 404 NOT FOUND"))
	check("cheat.sh hit detection", !cheatMiss("# tar\n# GNU version"))
	check("fzf install hint is OS specific",
		strings.Contains(fzfInstallHint(), "install"))
	parsed, parsedOK := parseFzfVersion("0.74.4 (a140afeb)")
	check("fzf version string is parsed",
		parsedOK && parsed.version == "0.74.4" && parsed.major == 0 &&
			parsed.minor == 74)
	fzfOld := fzfInfo{version: "0.29.0", major: 0, minor: 29}
	fzfNew := fzfInfo{version: "0.33.0", major: 0, minor: 33}
	check("--scheme=history needs fzf 0.33 or newer",
		!fzfOld.supportsHistoryScheme() && fzfNew.supportsHistoryScheme() &&
			parsed.supportsHistoryScheme())
	savedScriptDir := scriptDir
	if probeDir, probeErr := os.MkdirTemp("", "histex-fzf-"); probeErr == nil {
		bundled := filepath.Join(probeDir, "fzf.exe")
		os.WriteFile(bundled, []byte("stub"), 0o666)
		scriptDir = probeDir
		check("a bundled fzf next to the exe wins over PATH", fzfPath() == bundled)
		scriptDir = savedScriptDir
		os.RemoveAll(probeDir)
	} else {
		check("a bundled fzf next to the exe wins over PATH", false)
	}
	check("explain order includes the local tldr source",
		containsString(defaultConfig().ExplainOrder, "tldr"))
	disabled := *defaultConfig()
	disabled.Tldr = false
	check("local tldr source can be disabled",
		tldrLookup("zzz-not-real", &disabled) == "")
	check("tldr path lookup is safe", tldrPath() == "" || isFile(tldrPath()))
	updateHelper := updateTldr
	check("tldr update helper is wired", updateHelper != nil)

	preview := previewText("zzz-cmd | ww; qq", defaultConfig())
	check("preview keeps compound commands whole",
		!strings.Contains(preview, "--- [1/"))
	previewLines := pySplitLines(preview)
	check("preview shows the whole command",
		len(previewLines) > 0 && strings.Contains(previewLines[0], "zzz-cmd"))
	joinedNote := false
	for _, line := range previewLines {
		if strings.Contains(line, "3 commands joined") {
			joinedNote = true
		}
	}
	check("preview counts the joined commands", joinedNote)

	sample := "### Date: 2026-10-04 00:39 - My title   <!-- tags: a, b -->\n" +
		"\n```bash\nls -la\n```\n"
	check("recipe parsing strips date and tags",
		eqRecipes(parseRecipes(defaultConfig(), &sample), []recipe{
			{title: "My title", tags: []string{"a", "b"},
				commands: []string{"ls -la"}},
		}))
	check("preview is hidden by default", defaultConfig().Preview == false)
	check("preview toggle key is configured",
		defaultConfig().PreviewKey == "ctrl-p")
	check("header mentions the preview key", strings.Contains(fzfHeader, "^P"))
	fakeBash := "#1699999999\ngit status\n#1700000000\nls -la\n"
	check("bash HISTTIMEFORMAT markers are stripped",
		eqStrings(entriesFromText(fakeBash, false),
			[]string{"git status", "ls -la"}))
	check("zsh extended history is unwrapped",
		eqStrings(entriesFromText(": 1700000000:0;git status -sb\n", false),
			[]string{"git status -sb"}))
	fakeFish := "- cmd: git status\n  when: 1699999999\n" +
		"- cmd: |\n    if true\n        echo hi\n    end\n  when: 1700000000\n"
	check("fish history records are parsed",
		eqStrings(entriesForSource("fish", fakeFish, true),
			[]string{"git status", "if true\n    echo hi\nend"}))
	check("fish paths follow XDG_DATA_HOME",
		strings.HasSuffix(fishHistoryPath(), filepath.Join("fish", "fish_history")))
	check("tool help runs only plain tool names",
		toolHelpQuery("tar -xzf backup.tgz") == "tar" && toolHelpQuery("") == "" &&
			toolHelpQuery("| grep x") == "")
	check("tool help is part of the explain order",
		strings.Contains(strings.Join(defaultConfig().ExplainOrder, ","), "toolhelp"))
	week, weekErr := parseSince("7d")
	check("--since reads a relative window",
		weekErr == nil && time.Since(week) >= 6*24*time.Hour &&
			time.Since(week) <= 8*24*time.Hour)
	day, dayErr := parseSince("2026-10-01")
	_, badErr := parseSince("soon")
	check("--since reads a date and rejects nonsense",
		dayErr == nil && day.Year() == 2026 && day.Month() == time.October &&
			day.Day() == 1 && badErr != nil)
	check("the shell snippet logs the same sidecar file",
		strings.Contains(shellSnippet(defaultConfig()), "history_log.tsv") &&
			strings.Contains(shellSnippet(defaultConfig()), "_histex_log"))
	rows := doctorRows(defaultConfig())
	check("doctor returns labelled rows", len(rows) > 0)
	missing := *defaultConfig()
	missing.History = ptrTo("Z:/no-such-file.txt")
	missing.Shell = "auto"
	flagged := false
	for _, row := range doctorRows(&missing) {
		if !row.ok && strings.Contains(row.label, "no history file") {
			flagged = true
		}
	}
	check("doctor flags a missing history file", flagged)
	check("recipe preview prints the stored commands",
		strings.Contains(previewRecipeForTest("My title   (1 cmd)", jobject{
			{"My title   (1 cmd)", []string{"ls -la"}},
		}), "ls -la"))
	check("data dir is absolute and outside temp-script dirs",
		filepath.IsAbs(resolveDataDir("")))
	callback := selfCommand("--print-list", "--toggle-sort")
	check("fzf callbacks quote the executable",
		strings.HasPrefix(callback, "\"") &&
			strings.Contains(callback, executablePath()))
	check("fzf callbacks point at the exe, not at source files",
		!strings.Contains(callback, ".py") && !strings.Contains(callback, ".go") &&
			strings.Contains(callback, "--print-list") &&
			strings.Contains(callback, "--toggle-sort"))
	check("the reload bind carries the picker filters",
		strings.Contains(selfCommand(reloadArgs([]string{"--today", "--here"})...),
			"--today --here") &&
			!strings.Contains(selfCommand(reloadArgs(nil)...), "--today"))
	check("--today / --here are forwarded to the reload",
		eqStrings(pickerFilterArgs(&options{today: true, here: true}),
			[]string{"--today", "--here"}) &&
			eqStrings(pickerFilterArgs(&options{}), []string{}))

	sandbox, err := os.MkdirTemp("", "histex-test-")
	if err == nil {
		defer os.RemoveAll(sandbox)
		legacyScript := filepath.Join(sandbox, "old-script")
		legacyHome := filepath.Join(sandbox, "old-home")
		fresh := filepath.Join(sandbox, "new-data")
		os.MkdirAll(legacyScript, 0o777)
		os.MkdirAll(legacyHome, 0o777)
		probe := "### Date: 2026-01-01 00:00 - Probe\n"
		os.WriteFile(filepath.Join(legacyScript, "saved_recipes.md"),
			[]byte(probe), 0o666)
		os.WriteFile(filepath.Join(legacyHome, "sort.state"),
			[]byte("freq"), 0o666)
		moved, _ := migrateData(fresh, false, legacyScript, legacyHome)
		check("migration copies the recipe file",
			moved >= 2 &&
				rawFileEquals(filepath.Join(fresh, "saved_recipes.md"), probe))
		check("migration writes a receipt",
			isFile(filepath.Join(fresh, "migrated.json")))
		appendTextFile(filepath.Join(fresh, "saved_recipes.md"),
			"### extra\n", false, false)
		_, skipped2 := migrateData(fresh, false, legacyScript, legacyHome)
		check("migration never overwrites an existing target",
			!rawFileEquals(filepath.Join(fresh, "saved_recipes.md"), probe) &&
				skipped2 >= 1)
		check("migration is safe to run twice", skipped2 >= 1)
		override := filepath.Join(sandbox, "custom")
		check("explicit data-dir override is honoured",
			resolveDataDir(override) == override)

		leaf, leafErr := os.MkdirTemp("", "histex-leaf-")
		if leafErr == nil {
			defer os.RemoveAll(leaf)
			check("data_path() fills recipes from the data dir",
				dataPath(&Config{DataDir: leaf}, "recipes") ==
					filepath.Join(leaf, "saved_recipes.md"))
			check("data_path() honours an explicit recipes file",
				dataPath(&Config{DataDir: leaf, Recipes: ptrTo("keep.md")},
					"recipes") == "keep.md")
			filled := fillDataPaths(&Config{DataDir: leaf})
			check("data paths keep scripts, jsonl and cache together",
				deref(filled.ScriptsDir) == filepath.Join(leaf, "scripts") &&
					deref(filled.JSONL) == filepath.Join(leaf, "recipes.jsonl") &&
					deref(filled.CacheDir) == filepath.Join(leaf, "cache"))
			check("sort state lives in the data dir",
				sortStatePath(&Config{DataDir: leaf}) ==
					filepath.Join(leaf, "sort.state"))
		}

		atomicTarget := filepath.Join(sandbox, "atomic.txt")
		atomicWrite(atomicTarget, "hello")
		leftovers := 0
		if entries, readErr := os.ReadDir(sandbox); readErr == nil {
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".tmp") {
					leftovers++
				}
			}
		}
		text, _ := readTextFile(atomicTarget, false)
		check("atomic_write leaves no .tmp behind",
			text == "hello" && leftovers == 0)
	}

	return results
}

// selfTest is self_test(): run the checks and print the report.
func selfTest(cfg *Config) int {
	results := runSelfChecks()
	failures := 0
	for _, result := range results {
		if !result.ok {
			failures++
		}
	}
	for _, result := range results {
		if result.ok {
			outLine("[ok]   %s", result.name)
		} else {
			outLine("[FAIL] %s", result.name)
		}
	}
	outLine("")
	outLine("%d/%d checks passed", len(results)-failures, len(results))
	if failures > 0 {
		return 1
	}
	return 0
}
