package main

// selftest_test.go - `go test ./...` runs the same offline checks as
// `histex --self-test`, one subtest per check, plus a few table tests for the
// pure parsing helpers.

import (
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
