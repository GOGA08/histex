package main

// doctor.go - --doctor: one command that tells you what is broken.

import (
	"path/filepath"
	"runtime"
	"strings"
)

type doctorRow struct {
	label  string
	ok     bool
	detail string
	// info marks the rows that are neither a success nor a problem: the
	// optional extras. They used to print as [ok], which read like praise
	// for something that is simply missing (and optional).
	info bool
}

// state is the machine readable status of one row.
func (row doctorRow) state() string {
	switch {
	case !row.ok:
		return "error"
	case row.info:
		return "info"
	}
	return "ok"
}

// detectClipboard mirrors the clipboard detection shared by the doctor row.
func detectClipboard() string {
	if windowsNewlines {
		if lookWhich("clip") != "" {
			return "clip"
		}
		return ""
	}
	if runtime.GOOS == "darwin" {
		if lookWhich("pbcopy") != "" {
			return "pbcopy"
		}
		return ""
	}
	for _, name := range []string{"xclip", "wl-copy"} {
		if lookWhich(name) != "" {
			return name
		}
	}
	return ""
}

// doctorRows is doctor_rows(): pure data, so --self-test can check it too.
func doctorRows(cfg *Config) []doctorRow {
	rows := []doctorRow{}
	rows = append(rows, doctorRow{
		"go " + strings.TrimPrefix(runtime.Version(), "go"),
		true,
		executablePath(),
		false,
	})

	exe := fzfPath()
	switch {
	case exe == "":
		rows = append(rows, doctorRow{"fzf missing", false, fzfInstallHint(), false})
	default:
		if info, known := fzfVersion(exe); known {
			detail := exe
			if !info.supportsHistoryScheme() {
				detail = sprintf("%s   (fzf %s is older than 0.33: the picker "+
					"uses the default scoring scheme)", exe, info.version)
			}
			rows = append(rows, doctorRow{"fzf " + info.version, true, detail, false})
		} else {
			rows = append(rows, doctorRow{"fzf " + exe, true, exe, false})
		}
	}

	if texe := tldrPath(); texe != "" {
		rows = append(rows, doctorRow{"tldr " + texe, true,
			"run `tldr --update` once in a while", false})
	} else {
		rows = append(rows, doctorRow{"tldr missing (optional)", true,
			"winget install dbrgn.tealdeer  (or brew / apt)", true})
	}

	label, path := resolveHistory(cfg)
	if path != "" && isFile(path) {
		if _, _, entries, err := loadHistory(cfg, true); err != nil {
			rows = append(rows, doctorRow{"history unreadable", false,
				path + ": " + err.Error(), false})
		} else {
			rows = append(rows, doctorRow{
				sprintf("history (%s): %d unique commands", label, len(entries)),
				true, path, false})
		}
	} else {
		rows = append(rows, doctorRow{"no history file found", false,
			"run a few commands in your shell and try again", false})
	}

	clip := detectClipboard()
	clipDetail := clip
	if clipDetail == "" {
		clipDetail = "Windows: clip.exe ships with the OS; macOS: pbcopy; " +
			"Linux: xclip or wl-copy"
	}
	rows = append(rows, doctorRow{"clipboard: " + orNoTool(clip), clip != "",
		clipDetail, false})

	recipes := parseRecipes(cfg, nil)
	recipePath := deref(cfg.Recipes)
	if recipePath == "" {
		recipePath = dataPath(cfg, "recipes")
	}
	rows = append(rows, doctorRow{
		sprintf("recipes: %d saved", len(recipes)), true, recipePath, false})

	snippet := filepath.Join(rootOrScriptDir(cfg), dataFilenames["snippet"])
	log := filepath.Join(rootOrScriptDir(cfg), dataFilenames["sidecar"])
	if isFile(log) {
		rows = append(rows, doctorRow{"prompt snippet: installed", true,
			"sidecar log found: " + log, false})
	} else {
		rows = append(rows, doctorRow{"prompt snippet: not active", true,
			sprintf("run `histex --install-snippets`, dot-source %s from "+
				"$PROFILE, then open a new terminal", snippet), true})
	}
	return rows
}

func orNoTool(text string) string {
	if text == "" {
		return "no tool found"
	}
	return text
}

// doctorMode is doctor_mode(): health check with install hints, exit 1 broken.
// --json emits the same rows as a machine readable report on stdout.
func doctorMode(cfg *Config, asJSON bool) int {
	rows := doctorRows(cfg)
	bad := 0
	for _, row := range rows {
		if !row.ok {
			bad++
		}
	}
	if asJSON {
		items := []any{}
		for _, row := range rows {
			items = append(items, jobject{
				{"label", row.label},
				{"state", row.state()},
				{"detail", row.detail},
			})
		}
		payloadLine(pyJSON(jobject{
			{"histex", version},
			{"problems", bad},
			{"checks", items},
		}, "", 0))
		payloadLine("\n")
		if bad > 0 {
			return 1
		}
		return 0
	}

	outLine("%s", "histex "+version+"  (no browser - terminal and files only)")
	outLine("")
	for _, row := range rows {
		switch {
		case !row.ok:
			outLine("[x]  %s", row.label)
		case row.info:
			outLine("[i]  %s", row.label)
		default:
			outLine("[ok]  %s", row.label)
		}
		outLine("       %s", row.detail)
	}
	outLine("")
	if bad > 0 {
		outLine("%d problem(s) found - fix the [x] lines above.", bad)
		return 1
	}
	outLine("all good - run `%s` and press CTRL-P for the preview.", appName)
	return 0
}
