package main

// selftest_test.go - `go test ./...` runs the same offline checks as
// `histex --self-test`, one subtest per check, plus a few table tests for the
// pure parsing helpers.

import "testing"

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
