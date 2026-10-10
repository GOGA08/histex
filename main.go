package main

// main.go - the command line entry point and the dispatch order.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

type options struct {
	recipes      *string
	scriptsDir   *string
	jsonl        *string
	config       *string
	dataDir      *string
	history      *string
	shell        *string
	sort         *string
	detail       *string
	explain      *string
	preview      *string
	since        *string
	maxItems     *int
	noMigrate    bool
	today        bool
	here         bool
	offline      bool
	noCache      bool
	noPreview    bool
	showPreview  bool
	noTldr       bool
	updateTldr   bool
	browse       bool
	clean        bool
	restore      bool
	stats        bool
	pick         bool
	doctor       bool
	asJSON       bool
	selfTestFlag bool
	initConfig   bool
	installSnips bool
	printList    bool
	toggleSort   bool
	versionFlag  bool
}

// usage mirrors the argparse help closely enough to be familiar.
func usage(out io.Writer) {
	fmt.Fprintf(out, "usage: histex [options]\n\n")
	fmt.Fprintf(out, "Interactive picker for shell command history (fzf based).\n\n")
	fmt.Fprintf(out, "options:\n")
	lines := [][2]string{
		{"-h, --help", "show this help message and exit"},
		{"--version", "show the version and exit"},
		{"-r, --recipes FILE", "recipes markdown file"},
		{"--scripts-dir DIR", "where runnable scripts go"},
		{"--jsonl FILE", "machine readable recipe log"},
		{"--config FILE", "config file (default: <data-dir>/config.json)"},
		{"--data-dir DIR", "where recipes, scripts, cache and logs live"},
		{"--no-migrate", "do not copy data from the legacy locations"},
		{"--history FILE", "history file to read"},
		{"--shell SHELL", "which shell history to read (auto, ps5, ps7, bash, zsh, fish)"},
		{"--sort MODE", "initial ordering (recent, freq)"},
		{"--detail MODE", "explanation depth (short, full)"},
		{"--max-items N", "cap the number of entries"},
		{"--today", "only commands logged today (needs the profile snippet)"},
		{"--here", "only commands run in this folder (needs the snippet)"},
		{"--offline", "never use the network"},
		{"--no-cache", "ignore the local cache"},
		{"--no-preview", "disable the preview pane entirely (no toggle)"},
		{"--show-preview", "start with the preview pane visible"},
		{"--no-tldr", "do not use the local tldr pages"},
		{"--update-tldr", "refresh the local tldr page cache (tldr --update)"},
		{"--browse", "browse the saved recipes"},
		{"--clean", "delete history entries"},
		{"--restore", "put the --clean backup back in place"},
		{"--stats", "show usage statistics"},
		{"--since WINDOW", "with --stats: count only 90m, 24h, 7d, 4w or 2026-10-01"},
		{"--pick", "print only the chosen command(s) (shell integration)"},
		{"--doctor", "check the setup: tools, history, clipboard, recipes"},
		{"--json", "machine readable report (--stats, --doctor) or selection"},
		{"--explain CMD", "explain a command and exit"},
		{"--init-config", "write a config file"},
		{"--install-snippets", "write a profile snippet (PowerShell, or bash/zsh off Windows)"},
		{"--self-test", "run offline self checks"},
	}
	for _, line := range lines {
		fmt.Fprintf(out, "  %-24s %s\n", line[0], line[1])
	}
	fmt.Fprintf(out, "\nWith --pick (or --json) stdout carries only the payload:\n")
	fmt.Fprintf(out, "the chosen command(s), or the JSON report of --stats / --doctor.\n")
	fmt.Fprintf(out, "Everything else goes to stderr, so the shell wrappers can capture\n")
	fmt.Fprintf(out, "stdout without the human chatter getting in the way.\n")
	fmt.Fprintf(out, "\nKeys inside fzf:\n")
	fmt.Fprintf(out, "  ENTER   explain            TAB     mark / unmark\n")
	fmt.Fprintf(out, "  CTRL-T  save recipe        CTRL-O  copy to clipboard\n")
	fmt.Fprintf(out, "  CTRL-P  toggle preview     CTRL-R  reload + sort toggle\n")
}

func buildParser() (*flag.FlagSet, *options) {
	opts := &options{}
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { usage(os.Stderr) }

	opts.recipes = fs.String("recipes", "", "recipes markdown file")
	fs.StringVar(opts.recipes, "r", "", "recipes markdown file (short)")
	opts.scriptsDir = fs.String("scripts-dir", "", "where runnable scripts go")
	opts.jsonl = fs.String("jsonl", "", "machine readable recipe log")
	opts.config = fs.String("config", "", "config file")
	opts.dataDir = fs.String("data-dir", "", "where the user data lives")
	fs.BoolVar(&opts.noMigrate, "no-migrate", false, "do not migrate legacy data")
	opts.history = fs.String("history", "", "history file to read")
	opts.shell = fs.String("shell", "", "which shell history to read")
	opts.sort = fs.String("sort", "", "initial ordering")
	opts.detail = fs.String("detail", "", "explanation depth")
	opts.maxItems = fs.Int("max-items", 0, "cap the number of entries")
	fs.BoolVar(&opts.today, "today", false, "only commands logged today")
	fs.BoolVar(&opts.here, "here", false, "only commands run in this folder")
	fs.BoolVar(&opts.offline, "offline", false, "never use the network")
	fs.BoolVar(&opts.noCache, "no-cache", false, "ignore the local cache")
	fs.BoolVar(&opts.noPreview, "no-preview", false, "disable the preview pane")
	fs.BoolVar(&opts.showPreview, "show-preview", false, "show the preview pane")
	fs.BoolVar(&opts.noTldr, "no-tldr", false, "do not use local tldr pages")
	fs.BoolVar(&opts.updateTldr, "update-tldr", false, "refresh the tldr cache")
	fs.BoolVar(&opts.browse, "browse", false, "browse the saved recipes")
	fs.BoolVar(&opts.clean, "clean", false, "delete history entries")
	fs.BoolVar(&opts.restore, "restore", false, "put the --clean backup back")
	fs.BoolVar(&opts.stats, "stats", false, "show usage statistics")
	opts.since = fs.String("since", "", "with --stats: only count commands since a window")
	fs.BoolVar(&opts.pick, "pick", false, "print only the choice")
	fs.BoolVar(&opts.doctor, "doctor", false, "check the setup")
	fs.BoolVar(&opts.asJSON, "json", false, "machine readable report (--stats, --doctor) or selection")
	opts.explain = fs.String("explain", "", "explain a command and exit")
	opts.preview = fs.String("preview", "", "print text for the fzf preview pane")
	fs.BoolVar(&opts.printList, "print-list", false, "print the ready-to-use list")
	fs.BoolVar(&opts.toggleSort, "toggle-sort", false, "flip recent <-> freq")
	fs.BoolVar(&opts.initConfig, "init-config", false, "write a config file")
	fs.BoolVar(&opts.installSnips, "install-snippets", false, "write the profile snippet")
	fs.BoolVar(&opts.selfTestFlag, "self-test", false, "run offline self checks")
	fs.BoolVar(&opts.versionFlag, "version", false, "print the version and exit")
	return fs, opts
}

// pickerFilterArgs is the --today / --here the reload bind has to re-apply: it
// is a fresh process, so the filter can only come from its command line.
func pickerFilterArgs(opts *options) []string {
	args := []string{}
	if opts.today {
		args = append(args, "--today")
	}
	if opts.here {
		args = append(args, "--here")
	}
	return args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func main() {
	setupConsole()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		<-signals
		errLine("")
		errLine("[i] interrupted.")
		os.Exit(130)
	}()
	os.Exit(run())
}

func run() int {
	// The shell wrappers run `histex --pick ...` and capture stdout, so the
	// payload flags are recognised before any output at all: even --help has
	// to honour the contract (human text to stderr, payload to stdout).
	for _, arg := range os.Args[1:] {
		if arg == "--pick" || strings.HasPrefix(arg, "--pick=") {
			stdoutIsPayload = true
		}
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			stdoutIsPayload = true
			payloadAsJSON = true
		}
	}
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "-help" || arg == "--help" {
			usage(humanTarget())
			return 0
		}
	}

	parser, opts := buildParser()
	parser.Usage = func() {}
	if err := parser.Parse(os.Args[1:]); err != nil {
		usage(os.Stderr)
		return 2
	}
	set := map[string]bool{}
	parser.Visit(func(one *flag.Flag) { set[one.Name] = true })

	// The parsed flags are the authority; the pre-scan above only covered the
	// paths that run before parsing (like --help).
	stdoutIsPayload = opts.pick || opts.asJSON
	payloadAsJSON = opts.asJSON

	if opts.versionFlag {
		outLine("%s %s", appName, version)
		return 0
	}

	datadir := resolveDataDir(firstNonEmpty(*opts.dataDir,
		os.Getenv("HISTEX_DATA_DIR")))
	ensureDataDir(datadir)
	if !opts.noMigrate && !opts.toggleSort {
		migrateData(datadir, false, "", "")
	}
	cfg := loadConfig(*opts.config, datadir)

	// Only flags the user actually passed override the config file.
	if set["recipes"] {
		cfg.set("recipes", *opts.recipes)
	}
	if set["scripts-dir"] {
		cfg.set("scripts_dir", *opts.scriptsDir)
	}
	if set["jsonl"] {
		cfg.set("jsonl", *opts.jsonl)
	}
	if set["history"] {
		cfg.set("history", *opts.history)
	}
	if set["shell"] {
		cfg.Shell = *opts.shell
	}
	if set["sort"] {
		cfg.Sort = *opts.sort
	}
	if set["detail"] {
		cfg.Detail = *opts.detail
	}
	if set["max-items"] {
		cfg.MaxItems = *opts.maxItems
	}
	if opts.offline {
		cfg.Network = false
	}
	if opts.noCache {
		cfg.Cache = false
	}
	if opts.noPreview {
		cfg.Preview = false
		cfg.PreviewOff = true
	}
	if opts.showPreview {
		cfg.Preview = true
	}
	if opts.noTldr {
		cfg.Tldr = false
	}

	if opts.initConfig {
		path := writeDefaultConfig(firstNonEmpty(*opts.config, cfg.Path), cfg)
		outLine("[ok] config written to: %s", path)
		return 0
	}
	if opts.installSnips {
		return installSnippets(cfg)
	}
	if opts.doctor {
		return doctorMode(cfg, opts.asJSON)
	}
	if opts.selfTestFlag {
		return selfTest(cfg)
	}
	if opts.updateTldr {
		return updateTldr()
	}
	if set["preview"] {
		if os.Getenv("HISTEX_PREVIEW_MAP") != "" {
			recipesPreview(*opts.preview)
		} else {
			explain(*opts.preview, cfg, true)
		}
		return 0
	}
	if *opts.explain != "" {
		explain(*opts.explain, cfg, false)
		return 0
	}
	if opts.printList {
		printList(cfg, opts.toggleSort, opts.today, opts.here)
		return 0
	}
	if opts.browse {
		return recipesMode(cfg)
	}
	if opts.stats {
		return statsMode(cfg, *opts.since, opts.asJSON)
	}
	if set["since"] {
		errLine("[i] --since only applies to --stats: `histex --stats --since %s`", *opts.since)
	}
	if opts.clean {
		return cleanMode(cfg)
	}
	if opts.restore {
		return restoreMode(cfg)
	}

	label, path, entries, err := loadHistory(cfg, true)
	if err != nil {
		errLine("[x] could not read the history file: %s", err)
		return 1
	}
	if path == "" {
		errLine("[x] no history file found. Looked in:")
		for _, source := range historyCandidates() {
			errLine("    %-4s %s", source.label, source.path)
		}
		errLine("[i] run a few commands in your shell and try again.")
		return 1
	}
	if opts.today || opts.here {
		entries = filterBySidecar(entries, opts.today, opts.here, cfg)
	}
	if len(entries) == 0 {
		errLine("[i] nothing to show.")
		return 0
	}

	errLine("[i] %s   (%s)", path, label)
	errLine("    %d unique commands, sorted by %s", len(entries), cfg.Sort)
	// The header has to tell the truth about ENTER: in the wrappers (--pick /
	// --json) it emits the choice, it does not explain it.
	enterKey := "explain"
	if stdoutIsPayload {
		enterKey = "pick"
	}
	errLine("    keys: ENTER %s | TAB mark | CTRL-T save | CTRL-O copy | "+
		"CTRL-P preview | CTRL-R reload/sort | ESC cancel", enterKey)
	if recipes := parseRecipes(cfg, nil); len(recipes) > 0 {
		errLine("    recipes: %d saved - `histex --browse` opens the library",
			len(recipes))
	}

	status, key, selected := runFzf(entries, cfg, true, "", historyReloadArgs(pickerFilterArgs(opts)))
	if status == "error" {
		return 1
	}
	if status == "cancel" {
		outLine("[i] cancelled.")
		return 0
	}
	if len(selected) == 0 {
		outLine("[i] nothing selected.")
		return 0
	}

	// CTRL-O and CTRL-T act on the selection instead of emitting it, so they
	// have to win over --pick / --json: in the wrappers they keep working,
	// they just do not print a payload.
	if key == "ctrl-o" {
		doClipboard(selected)
		return 0
	}
	if key == "ctrl-t" || key == "ctrl-x" {
		saveRecipeFlow(selected, cfg)
		return 0
	}
	if opts.pick || opts.asJSON {
		// The destructive check still runs on stderr: a wrapper must never
		// paste a rm -rf without at least shouting about it.
		for _, command := range selected {
			if reason, found := dangerReason(command, cfg); found {
				errLine("[!] destructive pattern '%s' - be careful before running this.",
					reason)
			}
		}
		return emitSelection(selected, opts.asJSON)
	}

	for _, command := range selected {
		if reason, found := dangerReason(command, cfg); found {
			errLine("[!] destructive pattern '%s' - be careful before running this.",
				reason)
		}
	}
	for index, command := range selected {
		if index > 0 {
			outLine("")
			outLine("%s", strings.Repeat("-", 60))
		}
		explain(command, cfg, false)
	}
	return 0
}
