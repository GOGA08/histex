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

// shellSnippetName is the bash / zsh counterpart of the PowerShell snippet.
const shellSnippetName = "histex_profile.sh"

// shellSnippet is profileSnippet for bash and zsh: the picker function plus the
// sidecar log that --today and --here read.
func shellSnippet(cfg *Config) string {
	root := rootOrScriptDir(cfg)
	logFile := filepath.Join(root, dataFilenames["sidecar"])
	lines := []string{
		"# --- histex integration (bash / zsh) " + strings.Repeat("-", 41),
		"# Add this line to ~/.bashrc (bash) or ~/.zshrc (zsh), then open a new shell:",
		"#     . \"" + filepath.Join(root, shellSnippetName) + "\"",
		"#",
		"# It gives you two things:",
		"#   1) `histex` - pick a command and it lands on your prompt",
		"#   2) a sidecar log (time + directory + command) that --today and --here read",
		"",
		"HISTEX_BIN=\"" + executablePath() + "\"",
		"HISTEX_LOG=\"" + logFile + "\"",
		"",
		"# 1) pick a command and put it on the prompt",
		"histex() {",
		"    local picked",
		"    picked=$(\"$HISTEX_BIN\" --pick \"$@\") || return $?",
		"    [ -n \"$picked\" ] || return 0",
		"    if [ -n \"$ZSH_VERSION\" ]; then",
		"        print -z -- \"$picked\"       # zsh: push it onto the editing buffer",
		"    elif [ -n \"$READLINE_LINE\" ]; then",
		"        READLINE_LINE=\"$picked\"     # bash: needs the bind -x line below",
		"        READLINE_POINT=${#READLINE_LINE}",
		"    else",
		"        printf '%s\\n' \"$picked\"",
		"    fi",
		"}",
		"# bash users: bind it to a key so the pick lands on the prompt",
		"#     bind -x '\"\\C-g\": histex'",
		"",
		"# 2) the sidecar log. Override _histex_last_command to test the log without",
		"#    a real shell history.",
		"_histex_last_command() {",
		"    fc -ln -1 2>/dev/null",
		"}",
		"",
		"_histex_log() {",
		"    local line command",
		"    line=$(_histex_last_command)",
		"    line=${line#\"${line%%[![:space:]]*}\"}   # drop the leading spaces",
		"    line=${line#* }                         # drop the history number",
		"    [ -n \"$line\" ] || return 0",
		"    command=$(printf '%s' \"$line\" | tr '\\t\\n' '  ')",
		"    printf '%s\\t%s\\t%s\\n' \"$(date '+%Y-%m-%d %H:%M:%S')\" \"$PWD\" \"$command\" >>\"$HISTEX_LOG\"",
		"}",
		"",
		"if [ -n \"$BASH_VERSION\" ]; then",
		"    PROMPT_COMMAND=\"_histex_log${PROMPT_COMMAND:+; $PROMPT_COMMAND}\"",
		"elif [ -n \"$ZSH_VERSION\" ]; then",
		"    autoload -Uz add-zsh-hook",
		"    add-zsh-hook precmd _histex_log",
		"fi",
	}
	return strings.Join(lines, "\n") + "\n"
}

// installShellSnippet writes histex_profile.sh - the bash / zsh side.
func installShellSnippet(cfg *Config) int {
	path := filepath.Join(rootOrScriptDir(cfg), shellSnippetName)
	// no BOM and no newline translation: a CRLF shell script does not run
	if err := writeTextFile(path, shellSnippet(cfg), false, false); err != nil {
		errLine("[x] could not write the snippet: %s", err)
		return 1
	}
	outLine("Snippet written to: %s", path)
	outLine("")
	outLine("Nothing was changed on your system. To enable it, add this single")
	outLine("line to ~/.bashrc (bash) or ~/.zshrc (zsh):")
	outLine("")
	outLine("   . \"%s\"", path)
	outLine("")
	outLine("Then open a new shell. To undo: delete the file and the line.")
	return 0
}

// installSnippets is install_snippets(): writes the file, never $PROFILE.
func installSnippets(cfg *Config) int {
	if !windowsNewlines {
		return installShellSnippet(cfg)
	}
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
