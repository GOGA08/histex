package main

// selftest_test.go - `go test ./...` runs the same offline checks as
// `histex --self-test`, one subtest per check, plus a few table tests for the
// pure parsing helpers.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOfflineChecks runs the shared check list, so CI, --self-test and
// `go test` all cover the same ground.
func TestOfflineChecks(t *testing.T) {
	results := runSelfChecks()
	if len(results) < 50 {
		t.Fatalf("only %d checks ran, the full list is expected", len(results))
	}
	for _, result := range results {
		if !result.ok {
			t.Errorf("check failed: %s", result.name)
		}
	}
	t.Logf("%d checks passed", len(results))
}

func TestHistoryReloadArgs(t *testing.T) {
	cases := []struct {
		filter []string
		want   []string
	}{
		{nil, []string{"--print-list", "--toggle-sort"}},
		{[]string{"--today"}, []string{"--print-list", "--toggle-sort", "--today"}},
		{[]string{"--today", "--here"},
			[]string{"--print-list", "--toggle-sort", "--today", "--here"}},
	}
	for _, tc := range cases {
		if got := historyReloadArgs(tc.filter); !eqStrings(got, tc.want) {
			t.Errorf("historyReloadArgs(%q) = %q, want %q", tc.filter, got, tc.want)
		}
	}
	if args := pickerFilterArgs(&options{}); len(args) != 0 {
		t.Errorf("pickerFilterArgs on the plain picker = %q, want none", args)
	}
	if args := pickerFilterArgs(&options{today: true}); !eqStrings(args, []string{"--today"}) {
		t.Errorf("pickerFilterArgs(--today) = %q", args)
	}
}

func TestRecipeReloadEntries(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "recipes.txt"
	os.WriteFile(path, []byte(listText([]string{"one", "two   (2 cmd)"})), 0o644)
	t.Setenv("HISTEX_RECIPES_LIST", path)
	got, isRecipes := recipeReloadEntries()
	if !isRecipes || !eqStrings(got, []string{"one", "two   (2 cmd)"}) {
		t.Errorf("recipeReloadEntries = %q (isRecipes=%v)", got, isRecipes)
	}
	t.Setenv("HISTEX_RECIPES_LIST", "")
	if _, isRecipes = recipeReloadEntries(); isRecipes {
		t.Error("recipeReloadEntries must not fire without the marker")
	}
}

func TestValidScriptFileName(t *testing.T) {
	cases := []struct {
		name, ext, want string
		ok              bool
	}{
		{"my-script", ".ps1", "my-script.ps1", true},
		{"my-script.ps1", ".ps1", "my-script.ps1", true},
		{"MY-SCRIPT.PS1", ".ps1", "MY-SCRIPT.PS1", true},
		{"  spaced  ", ".sh", "spaced.sh", true},
		{`"quoted.ps1"`, ".ps1", "quoted.ps1", true},
		{"", ".ps1", "", false},
		{"..", ".ps1", "", false},
		{"a<b.ps1", ".ps1", "", false},
		{"bad|name.ps1", ".ps1", "", false},
		{"CON.ps1", ".ps1", "", false},
		{"lpt1", ".bat", "", false},
		{"trailing.", ".sh", "", false},
	}
	for _, tc := range cases {
		got, ok := validScriptFileName(tc.name, tc.ext)
		if ok != tc.ok || got != tc.want {
			t.Errorf("validScriptFileName(%q, %q) = %q, %v; want %q, %v",
				tc.name, tc.ext, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveSaveDir(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o777); err != nil {
		t.Fatal(err)
	}
	pick := func(string) (string, bool) { return sub, true }
	cases := []struct {
		answer string
		want   string
		ok     bool
	}{
		{"sub", sub, true},
		{"cd sub", sub, true},
		{"CD sub", sub, true},
		{`"sub"`, sub, true},
		{".", base, true},
		{"..", filepath.Dir(base), true},
		{"...", sub, true},
		{"", "", false},
		{"cancel", "", false},
		{"nope/deeper", "", false},
		{"brand-new-folder", filepath.Join(base, "brand-new-folder"), true},
		{base, base, true},
	}
	for _, tc := range cases {
		got, ok := resolveSaveDir(tc.answer, base, pick)
		if ok != tc.ok || (ok && !samePath(got, tc.want)) {
			t.Errorf("resolveSaveDir(%q) = %q, %v; want %q, %v",
				tc.answer, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := resolveSaveDir("...", base, nil); ok {
		t.Error("resolveSaveDir(...) without a picker must fail")
	}
}

func TestListChildDirs(t *testing.T) {
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "alpha"), 0o777)
	os.MkdirAll(filepath.Join(base, "beta"), 0o777)
	os.WriteFile(filepath.Join(base, "note.txt"), []byte("x"), 0o666)
	got := listChildDirs(base, 1000)
	if !eqStrings(got, []string{"..", "alpha", "beta"}) {
		t.Errorf("listChildDirs = %q", got)
	}
	if limited := listChildDirs(base, 1); !eqStrings(limited, []string{".."}) {
		t.Errorf("listChildDirs with limit = %q", limited)
	}
}

func TestScriptPayloadBOM(t *testing.T) {
	commands := []string{"echo hi"}
	ps1, _ := scriptPayload(commands, ".ps1")
	if len(ps1) < 3 || ps1[0] != 0xEF || ps1[1] != 0xBB || ps1[2] != 0xBF {
		t.Error(".ps1 must start with a UTF-8 BOM")
	}
	if !strings.Contains(string(ps1), "#Requires -Version 5.1") {
		t.Error(".ps1 must carry the PowerShell header")
	}
	for _, ext := range []string{".bat", ".cmd", ".sh"} {
		payload, _ := scriptPayload(commands, ext)
		if len(payload) >= 3 && payload[0] == 0xEF {
			t.Errorf("%s must not start with a BOM", ext)
		}
	}
}

func TestTruncateFileTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saved_recipes.md")
	os.WriteFile(path, []byte("keep me\n"), 0o666)
	size := atomicFileSize(path)
	appendTextFile(path, "lost line\n", false, false)
	if got := atomicFileSize(path); got <= size {
		t.Fatalf("append did not grow the file: %d", got)
	}
	if err := truncateFileTo(path, size); err != nil {
		t.Fatal(err)
	}
	if !rawFileEquals(path, "keep me\n") {
		t.Error("truncateFileTo must restore the previous bytes")
	}
	if err := truncateFileTo(path, -1); err != nil {
		t.Fatal(err)
	}
	if isFile(path) {
		t.Error("truncateFileTo(-1) must remove a file that did not exist")
	}
}

func TestAskSaveDirLoop(t *testing.T) {
	base := t.TempDir()
	os.MkdirAll(filepath.Join(base, "sub"), 0o777)
	pick := func(string) (string, bool) { return filepath.Join(base, "sub"), true }
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{"\n", base, true},
		{"cd sub\n\n", filepath.Join(base, "sub"), true},
		{"sub\n\n", filepath.Join(base, "sub"), true},
		{"new-folder\n\n", filepath.Join(base, "new-folder"), true},
		{"no/deeper\n\n", base, true},
		{"...\n\n", filepath.Join(base, "sub"), true},
		{"cancel\n", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		previous := stdinReader
		stdinReader = bufio.NewReader(strings.NewReader(tc.input))
		got, ok := captureStderrAskSaveDir(tc.input, base, pick)
		stdinReader = previous
		if ok != tc.ok || (ok && !samePath(got, tc.want)) {
			t.Errorf("askSaveDir(%q) = %q, %v; want %q, %v",
				tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

// captureStderrAskSaveDir runs askSaveDir with a fresh stdin and swallows the
// prompt text, which goes to stdout and stderr.
func captureStderrAskSaveDir(input string, base string, pick func(string) (string, bool)) (string, bool) {
	oldStdout, oldStderr := os.Stdout, os.Stderr
	reader, writer, err := os.Pipe()
	if err == nil {
		os.Stdout = writer
		os.Stderr = writer
		defer func() {
			writer.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr
			reader.Close()
		}()
		go io.Copy(io.Discard, reader)
	}
	stdinReader = bufio.NewReader(strings.NewReader(input))
	return askSaveDir("save script to", base, pick)
}

// withSilentPrompt runs fn with a scripted keyboard and the prompt text on a
// pipe, so tests can drive the real interactive flow without touching a
// terminal. The stdin reader is restored afterwards.
func withSilentPrompt(input string, fn func()) {
	oldStdout, oldStderr, oldStdin := os.Stdout, os.Stderr, stdinReader
	reader, writer, err := os.Pipe()
	if err == nil {
		os.Stdout, os.Stderr = writer, writer
		go io.Copy(io.Discard, reader)
	}
	stdinReader = bufio.NewReader(strings.NewReader(input))
	defer func() {
		os.Stdout, os.Stderr, stdinReader = oldStdout, oldStderr, oldStdin
		if err == nil {
			writer.Close()
			reader.Close()
		}
	}()
	fn()
}

// runSaveFlow drives saveRecipeFlow with a scripted keyboard and swallows the
// prompts, so the real file writes can be checked.
func runSaveFlow(cfg *Config, input string) bool {
	saved := false
	withSilentPrompt(input, func() {
		saved = saveRecipeFlow([]string{"git status"}, cfg)
	})
	return saved
}

func TestResolveScriptTargetOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "notes.ps1")
	os.WriteFile(existing, []byte("old\n"), 0o666)
	run := func(answer string, input string) (string, bool) {
		target, ok := "", false
		withSilentPrompt(input, func() {
			target, ok = resolveScriptTarget(answer, dir, ".ps1")
		})
		return target, ok
	}
	if target, ok := run("fresh", ""); !ok || filepath.Base(target) != "fresh.ps1" {
		t.Errorf("a fresh name must be accepted without a question: %q, %v", target, ok)
	}
	if _, ok := run("notes.ps1", "n\n"); ok {
		t.Error("declining the overwrite must abort")
	}
	if _, ok := run("notes.ps1", ""); ok {
		t.Error("EOF at the overwrite question must abort")
	}
	if target, ok := run("notes.ps1", "y\n"); !ok || !samePath(target, existing) {
		t.Errorf("confirming the overwrite must return the path: %q, %v", target, ok)
	}
	if _, ok := run("", ""); ok {
		t.Error("an empty file answer must abort")
	}
}

func TestSaveRecipeFlowWritesNothingWhenCancelled(t *testing.T) {
	dataDir := t.TempDir()
	outDir := t.TempDir()
	cfg := defaultConfig()
	cfg.DataDir = dataDir
	recipes := filepath.Join(dataDir, "saved_recipes.md")
	jsonl := filepath.Join(dataDir, "recipes.jsonl")

	cases := []struct {
		name  string
		input string
	}{
		{"eof at the title", ""},
		{"eof at the choice", "My Title\n\n"},
		{"cancel at the destination", "My Title\n\n2\ncancel\n"},
		{"no script destination answer", "My Title\n\n2\n"},
		{"bad file name", "My Title\n\n2\n" + outDir + "\n../evil..\n"},
	}
	for _, tc := range cases {
		if runSaveFlow(cfg, tc.input) {
			t.Errorf("%s: saveRecipeFlow must report nothing saved", tc.name)
		}
		if isFile(recipes) {
			t.Errorf("%s: the recipe file must not exist", tc.name)
		}
		if isFile(jsonl) {
			t.Errorf("%s: the jsonl log must not exist", tc.name)
		}
		entries, err := os.ReadDir(filepath.Join(dataDir, "scripts"))
		if err == nil && len(entries) > 0 {
			t.Errorf("%s: no script may be written, found %d", tc.name, len(entries))
		}
		if leftovers, err := os.ReadDir(outDir); err == nil && len(leftovers) > 0 {
			t.Errorf("%s: no script may reach the chosen folder", tc.name)
		}
	}
	if !runSaveFlow(cfg, "My Title\n\n\n") {
		t.Fatal("a markdown-only save must succeed")
	}
	markdown, _ := readTextFile(recipes, true)
	if !strings.Contains(markdown, "### Date: ") || !strings.Contains(markdown, "My Title") {
		t.Errorf("the recipe file is missing the heading: %q", markdown)
	}
	log, _ := readTextFile(jsonl, true)
	if !strings.Contains(log, "\"scripts\": []") || !strings.Contains(log, "git status") {
		t.Errorf("the jsonl mirror is wrong: %q", log)
	}
	if entries, err := os.ReadDir(filepath.Join(dataDir, "scripts")); err == nil && len(entries) > 0 {
		t.Error("markdown-only must not create a script")
	}
}

func TestSaveRecipeFlowWritesTheChosenScript(t *testing.T) {
	dataDir := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "nested")
	cfg := defaultConfig()
	cfg.DataDir = dataDir
	input := "My Title\n\n2\n" + outDir + "\n\nmy notes\n"
	if !runSaveFlow(cfg, input) {
		t.Fatal("saving with a script must succeed")
	}
	target := filepath.Join(outDir, "my notes.ps1")
	if !isFile(target) {
		t.Fatalf("the script was not written to the chosen folder: %s", target)
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) < 3 || payload[0] != 0xEF || payload[1] != 0xBB || payload[2] != 0xBF {
		t.Error("the .ps1 must start with a UTF-8 BOM")
	}
	if !strings.Contains(string(payload), "git status") {
		t.Error("the .ps1 must carry the commands")
	}
	log, _ := readTextFile(filepath.Join(dataDir, "recipes.jsonl"), true)
	if !strings.Contains(log, filepath.Base(target)) {
		t.Errorf("the jsonl mirror must point at the script: %q", log)
	}
	markdown, _ := readTextFile(filepath.Join(dataDir, "saved_recipes.md"), true)
	if !strings.Contains(markdown, "<!-- scripts: "+target+" -->") {
		t.Errorf("the markdown history must record where the script lives: %q", markdown)
	}
	stored := parseRecipes(cfg, nil)
	if len(stored) != 1 || !eqStrings(stored[0].scripts, []string{target}) {
		t.Errorf("the recorded path must be read back: %+v", stored)
	}
	if label := recipeLabel(stored[0]); !strings.Contains(label, target) {
		t.Errorf("the library line must show the folder: %q", label)
	}
	if runSaveFlow(cfg, "N\n") {
		t.Error("a declined duplicate must not save anything")
	}
	markdown, _ = readTextFile(filepath.Join(dataDir, "saved_recipes.md"), true)
	if strings.Count(markdown, "### Date: ") != 1 {
		t.Error("declining the duplicate must not add a second block")
	}
	if !runSaveFlow(cfg, "y\nMy Title\n\n1\n") {
		t.Error("an explicit yes on the duplicate must save")
	}
	markdown, _ = readTextFile(filepath.Join(dataDir, "saved_recipes.md"), true)
	if strings.Count(markdown, "### Date: ") != 2 {
		t.Error("an explicit yes must add a second block")
	}
}
func TestIsAffirmative(t *testing.T) {

	for _, yes := range []string{"y", "Y", "yes", "YES", "1", "true", " ki "} {
		if !isAffirmative(yes) {
			t.Errorf("isAffirmative(%q) must be true", yes)
		}
	}
	for _, no := range []string{"", "n", "no", "nah", "0", "false", "cancel"} {
		if isAffirmative(no) {
			t.Errorf("isAffirmative(%q) must be false", no)
		}
	}
}
func TestParseFzfVersion(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		parsed bool
	}{
		{"0.74.4 (a140afeb)", "0.74.4", true},
		{"v0.29.0", "0.29.0", true},
		{"0.33", "0.33", true},
		{"", "", false},
		{"not-a-version", "", false},
	}
	for _, tc := range cases {
		info, ok := parseFzfVersion(tc.in)
		if ok != tc.parsed {
			t.Errorf("parseFzfVersion(%q): parsed = %v, want %v", tc.in, ok, tc.parsed)
			continue
		}
		if ok && info.version != tc.want {
			t.Errorf("parseFzfVersion(%q): version = %q, want %q", tc.in, info.version, tc.want)
		}
	}
	if old := (fzfInfo{version: "0.29.0", major: 0, minor: 29}); old.supportsHistoryScheme() {
		t.Error("fzf 0.29 must not be treated as supporting --scheme=history")
	}
	if fresh := (fzfInfo{version: "0.33.0", major: 0, minor: 33}); !fresh.supportsHistoryScheme() {
		t.Error("fzf 0.33 must be treated as supporting --scheme=history")
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\nb", []string{"a", "b"}},
		{"one", []string{"one"}},
		{"", nil},
	}
	for _, tc := range cases {
		got := pySplitLines(tc.in)
		if len(got) != len(tc.want) || !eqStrings(got, tc.want) {
			t.Errorf("pySplitLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFishHistoryEntries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain records", "- cmd: ls -la\n  when: 1\n- cmd: git status\n  when: 2\n",
			[]string{"ls -la", "git status"}},
		{"block scalar keeps the layout",
			"- cmd: |\n    if true\n        echo hi\n    end\n  when: 3\n",
			[]string{"if true\n    echo hi\nend"}},
		{"comments and blanks are ignored", "\n# comment\n", []string{}},
		{"quoted scalar is unwrapped",
			"- cmd: \"git commit -m \\\"hi\\\"\"\n  when: 4\n",
			[]string{`git commit -m "hi"`}},
	}
	for _, tc := range cases {
		got := entriesForSource("fish", tc.in, true)
		if !eqStrings(got, tc.want) {
			t.Errorf("%s: entriesForSource = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseSince(t *testing.T) {
	for _, bad := range []string{"", "soon", "-3d"} {
		if _, err := parseSince(bad); err == nil {
			t.Errorf("parseSince(%q) must fail", bad)
		}
	}
	week, err := parseSince("7d")
	if err != nil {
		t.Fatalf("parseSince(7d): %v", err)
	}
	if age := time.Since(week); age < 6*24*time.Hour || age > 8*24*time.Hour {
		t.Errorf("parseSince(7d) is %v old, want about a week", age)
	}
	date, err := parseSince("2026-10-01")
	if err != nil || date.Day() != 1 || date.Month() != time.October {
		t.Errorf("parseSince(2026-10-01) = %v, %v", date, err)
	}
}

func TestToolHelpQuery(t *testing.T) {
	cases := map[string]string{
		"tar -xzf a.tgz": "tar",
		"   ":            "",
		"| grep x":       "",
		"--weird":        "",
	}
	for in, want := range cases {
		if got := toolHelpQuery(in); got != want {
			t.Errorf("toolHelpQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunHelpUnderPickKeepsStdoutClean covers the wrapper contract end to end:
// `histex --pick --help` must not put the usage text on stdout, or the
// PowerShell/bash wrapper would paste it into the prompt.
func TestRunHelpUnderPickKeepsStdoutClean(t *testing.T) {
	savedArgs := os.Args
	defer func() {
		os.Args = savedArgs
		stdoutIsPayload, payloadAsJSON = false, false
	}()
	os.Args = []string{"histex", "--pick", "--help"}
	out, errOut := captureBoth(func() { run() })
	if strings.TrimSpace(out) != "" {
		t.Errorf("--pick --help printed to stdout: %q", out)
	}
	if !strings.Contains(errOut, "usage: histex") {
		t.Errorf("--pick --help must print the usage on stderr, got %q", errOut)
	}
}

// TestRunVersionUnderJSON: --version is human text, so with --json it moves
// to stderr and stdout stays an empty payload.
func TestRunVersionUnderJSON(t *testing.T) {
	savedArgs := os.Args
	defer func() {
		os.Args = savedArgs
		stdoutIsPayload, payloadAsJSON = false, false
	}()
	os.Args = []string{"histex", "--version", "--json"}
	out, errOut := captureBoth(func() { run() })
	if strings.TrimSpace(out) != "" {
		t.Errorf("--version --json printed to stdout: %q", out)
	}
	if !strings.Contains(errOut, appName) || !strings.Contains(errOut, version) {
		t.Errorf("--version --json must print the version on stderr, got %q", errOut)
	}
}

// TestStatsJSONReport: --stats --json is one JSON object on stdout with the
// numbers of the human report.
func TestStatsJSONReport(t *testing.T) {
	dir := t.TempDir()
	history := filepath.Join(dir, "history.txt")
	os.WriteFile(history, []byte("git status\nls -la\ngit status\n"), 0o644)
	cfg := defaultConfig()
	cfg.History = ptrTo(history)

	out, errOut := captureBoth(func() { statsMode(cfg, "", true) })
	report := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("--stats --json output is not JSON: %v\n%s", err, out)
	}
	if errOut != "" {
		t.Errorf("--stats --json wrote human text to stderr: %q", errOut)
	}
	entries, ok := report["entries"].(map[string]any)
	if !ok {
		t.Fatalf("--stats --json report has no entries object: %s", out)
	}
	if entries["total"] != float64(3) || entries["unique"] != float64(2) {
		t.Errorf("entries = %v, want total 3 / unique 2", entries)
	}
	if _, ok := report["top_commands"].([]any); !ok {
		t.Errorf("--stats --json report has no top_commands array: %s", out)
	}
	if _, ok := report["source"].(map[string]any); !ok {
		t.Errorf("--stats --json report has no source object: %s", out)
	}
}

// TestStatsHumanReportKeepsItsLayout: the human mode must not change shape
// because of the --json branch.
func TestStatsHumanReportKeepsItsLayout(t *testing.T) {
	dir := t.TempDir()
	history := filepath.Join(dir, "history.txt")
	os.WriteFile(history, []byte("git status\nls -la\ngit status\n"), 0o644)
	cfg := defaultConfig()
	cfg.History = ptrTo(history)

	out, errOut := captureBoth(func() { statsMode(cfg, "", false) })
	for _, want := range []string{"entries      : 3 total, 2 unique",
		"top 15 commands:", "top 15 tools:", "explain order :"} {
		if !strings.Contains(out, want) {
			t.Errorf("--stats human report misses %q:\n%s", want, out)
		}
	}
	if errOut != "" {
		t.Errorf("--stats wrote to stderr: %q", errOut)
	}
}

// TestDoctorJSONReport: --doctor --json mirrors the rows as objects with a
// machine readable state.
func TestDoctorJSONReport(t *testing.T) {
	out, errOut := captureBoth(func() { doctorMode(defaultConfig(), true) })
	report := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("--doctor --json output is not JSON: %v\n%s", err, out)
	}
	if errOut != "" {
		t.Errorf("--doctor --json wrote human text to stderr: %q", errOut)
	}
	checks, ok := report["checks"].([]any)
	if !ok || len(checks) == 0 {
		t.Fatalf("--doctor --json report has no checks: %s", out)
	}
	for _, one := range checks {
		row, isObject := one.(map[string]any)
		if !isObject {
			t.Fatalf("doctor check is not an object: %v", one)
		}
		state, _ := row["state"].(string)
		if state != "ok" && state != "info" && state != "error" {
			t.Errorf("doctor check state = %q, want ok / info / error", state)
		}
		if _, hasLabel := row["label"]; !hasLabel {
			t.Errorf("doctor check misses the label: %v", row)
		}
	}
	if _, ok := report["problems"].(float64); !ok {
		t.Errorf("--doctor --json report has no numeric problems: %s", out)
	}
}

// TestPickerHeader follows the payload mode.
func TestPickerHeader(t *testing.T) {
	defer func() { stdoutIsPayload = false }()
	stdoutIsPayload = false
	if header := pickerHeader(); header != fzfHeader {
		t.Errorf("pickerHeader() = %q, want %q", header, fzfHeader)
	}
	stdoutIsPayload = true
	header := pickerHeader()
	if !strings.Contains(header, "ENTER pick") || strings.Contains(header, "ENTER explain") {
		t.Errorf("pickerHeader() under --pick = %q, want ENTER pick", header)
	}
}

// TestDoctorRowStates pins the three-state machine of the doctor rows.
func TestDoctorRowStates(t *testing.T) {
	cases := []struct {
		row  doctorRow
		want string
	}{
		{doctorRow{label: "go", ok: true}, "ok"},
		{doctorRow{label: "tldr missing (optional)", ok: true, info: true}, "info"},
		{doctorRow{label: "fzf missing", ok: false}, "error"},
		{doctorRow{label: "x", ok: false, info: true}, "error"},
	}
	for _, tc := range cases {
		if got := tc.row.state(); got != tc.want {
			t.Errorf("doctorRow(%q, ok=%v, info=%v).state() = %q, want %q",
				tc.row.label, tc.row.ok, tc.row.info, got, tc.want)
		}
	}
}
