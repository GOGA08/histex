package main

// guards.go - the safety guards and the PowerShell based lookups.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// matchesAny is matches_any(): returns the pattern that matched, if any.
func matchesAny(text string, patterns []string) (string, bool) {
	for _, pattern := range patterns {
		compiled := compileSearch(pattern)
		if compiled == nil {
			continue
		}
		if compiled.MatchString(text) {
			return pattern, true
		}
	}
	return "", false
}

// isSecret is is_secret(): the command looks like it holds a secret.
func isSecret(entry string, cfg *Config) (string, bool) {
	return matchesAny(entry, cfg.Secrets)
}

// dangerReason is danger_reason(): the command looks destructive.
func dangerReason(entry string, cfg *Config) (string, bool) {
	return matchesAny(entry, cfg.Danger)
}

// normalizeToken is _normalize_token(): quotes, call operator and path.
func normalizeToken(token string) string {
	clean := strings.TrimSpace(token)
	clean = strings.Trim(clean, "\"'")
	clean = strings.TrimLeft(clean, "&")
	if strings.ContainsAny(clean, "/\\") {
		clean = filepath.Base(strings.ReplaceAll(clean, "\\", "/"))
	}
	return clean
}

var nameOK = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

var (
	aliasCache   = map[string]string{}
	aliasMissing = map[string]bool{}
)

// resolveAlias is resolve_alias(): maps a PowerShell alias to its definition.
func resolveAlias(name string, cfg *Config) (string, bool) {
	if name == "" || !windowsNewlines || !nameOK.MatchString(name) {
		return "", false
	}
	key := strings.ToLower(name)
	if value, cached := aliasCache[key]; cached {
		return value, value != ""
	}
	if aliasMissing[key] {
		return "", false
	}
	out := runPowerShell(fmt.Sprintf(
		"$a = Get-Alias -Name '%s' -ErrorAction SilentlyContinue; "+
			"if ($a) { $a.Definition }", name), 20)
	resolved := ""
	if strings.TrimSpace(out) != "" {
		lines := pySplitLines(out)
		if len(lines) > 0 {
			resolved = strings.TrimSpace(lines[0])
		}
	}
	aliasCache[key] = resolved
	if resolved == "" {
		aliasMissing[key] = true
	}
	return resolved, resolved != ""
}

// aliasWarning is alias_warning(): an alias that shadows a Unix tool.
func aliasWarning(name string, definition string) string {
	if definition == "" {
		return ""
	}
	lowered := strings.ToLower(name)
	if (lowered == "curl" || lowered == "wget") &&
		strings.ToLower(definition) != lowered {
		return fmt.Sprintf("'%s' in PowerShell is an alias for %s, not the real "+
			"%s tool - use %s.exe for the real one.", name, definition, name, name)
	}
	return ""
}

// psCommandType is ps_command_type(): Cmdlet / Function / Application / ...
func psCommandType(name string) string {
	if !windowsNewlines || !nameOK.MatchString(name) {
		return ""
	}
	out := runPowerShell(fmt.Sprintf(
		"$c = Get-Command -Name '%s' -ErrorAction SilentlyContinue; "+
			"if ($c) { $c.CommandType.ToString() }", name), 20)
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return strings.TrimSpace(pySplitLines(out)[0])
}

// localExplain is local_explain(): local PowerShell help, works offline.
// is_rich=false means the help database is thin, so cheat.sh is preferred.
func localExplain(command string, cfg *Config) (string, bool) {
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return "", false
	}
	name := normalizeToken(tokens[0])
	switch psCommandType(name) {
	case "Cmdlet", "Function", "Filter", "Configuration", "Script", "ExternalScript":
	default:
		return "", false
	}
	helpSwitch := "-Examples"
	if cfg.Detail == "full" {
		helpSwitch = "-Full"
	}
	out := runPowerShell(fmt.Sprintf(
		"(Get-Help -Name '%s' %s -ErrorAction SilentlyContinue | "+
			"Out-String -Width 200)", name, helpSwitch), 25)
	text := strings.TrimSpace(out)
	if stringWidth(text) < 40 {
		return "", false
	}
	rich := strings.Contains(strings.ToUpper(text), "EXAMPLE") && stringWidth(text) > 400
	return text, rich
}
