package main

// snippet.go - the PowerShell profile snippet (prompt integration + log).

import (
	"path/filepath"
	"strings"
)

// profileSnippet is profile_snippet(): histex <-> prompt integration and the
// sidecar log that enables --today / --here.
func profileSnippet(cfg *Config) string {
	logFile := filepath.Join(rootOrScriptDir(cfg), dataFilenames["sidecar"])
	launcher := quoted(executablePath())
	lines := []string{
		"# --- histex integration " + strings.Repeat("-", 52),
		"# 1) Type \"histex\", pick a command, and it lands on your prompt:",
		"function histex {",
		"    param([Parameter(ValueFromRemainingArguments = $true)]$Rest)",
		"    $picked = & " + launcher + " --pick @Rest",
		"    if ($picked) { [Microsoft.PowerShell.PSConsoleReadLine]::Insert($picked) }",
		"}",
		"",
		"# 2) Sidecar log (timestamp + directory). The plain PSReadLine history",
		"#    has neither, so this is what enables --today and --here.",
		"Set-PSReadLineOption -AddToHistoryHandler {",
		"    param($line)",
		"    $flat = ($line -replace \"`r?`n\", \" \").Replace(\"`t\", \" \")",
		"    $record = (Get-Date).ToString(\"yyyy-MM-dd HH:mm:ss\") + \"`t\" + " +
			"$PWD.Path + \"`t\" + $flat",
		"    Add-Content -LiteralPath \"" + logFile + "\" -Value $record -Encoding UTF8",
		"    return $true",
		"}",
	}
	return strings.Join(lines, "\n") + "\n"
}

// installSnippets is install_snippets(): writes the file, never $PROFILE.
func installSnippets(cfg *Config) int {
	path := filepath.Join(rootOrScriptDir(cfg), dataFilenames["snippet"])
	// utf-8-sig and newline="" in Python: BOM, no newline translation.
	if err := writeTextFile(path, profileSnippet(cfg), true, false); err != nil {
		errLine("[x] could not write the snippet: %s", err)
		return 1
	}
	profile := strings.TrimSpace(runPowerShell("$PROFILE", 20))
	if profile == "" {
		profile = "$PROFILE"
	}
	outLine("Snippet written to: %s", path)
	outLine("")
	outLine("Nothing was changed on your system. To enable it, add this single")
	outLine("line to your PowerShell profile (%s):", profile)
	outLine("")
	outLine("   . \"%s\"", path)
	outLine("")
	outLine("Then open a new terminal. To undo: delete the file and the line.")
	return 0
}
