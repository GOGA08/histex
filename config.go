package main

// config.go - data dir policy, the config file, and the one-time migration.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const appName = "histex"

// version of histex (kept identical to the Python one).
// version of histex. A plain `go build` reports 2.0; the release workflow
// overrides it from the git tag, so the binary always matches its release:
//
//	go build -ldflags "-X main.version=2.5" .
var version = "2.5"

var (
	scriptDir     = exeDir()
	homeDirPath   = homeDir()
	legacyHomeDir = filepath.Join(homeDirPath, ".histex")
)

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// baseRoots mirrors base_roots(): (appdata_root, script_dir).
func baseRoots() (string, string) {
	return os.Getenv("APPDATA"), scriptDir
}

// resolveDataDir mirrors resolve_data_dir(): the same five-step policy.
func resolveDataDir(override string) string {
	if override != "" {
		return absPath(override)
	}
	if isFile(filepath.Join(scriptDir, "histex.portable")) {
		return scriptDir
	}
	var candidates []string
	if windowsNewlines {
		appdata, _ := baseRoots()
		if appdata != "" {
			candidates = append(candidates, filepath.Join(appdata, "histex"))
		}
	} else {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			candidates = append(candidates, filepath.Join(xdg, "histex"))
		} else {
			candidates = append(candidates, filepath.Join(homeDirPath, ".local", "share", "histex"))
		}
		candidates = append(candidates, legacyHomeDir)
	}
	for _, candidate := range candidates {
		if writableDir(candidate) {
			return candidate
		}
	}
	fallback := filepath.Join(scriptDir, "data")
	if writableDir(fallback) {
		return fallback
	}
	return filepath.Join(os.TempDir(), "histex")
}

// writableDir is _writable_dir(): exists (or can be made) and is writable.
func writableDir(path string) bool {
	if err := os.MkdirAll(path, 0o777); err != nil {
		return false
	}
	probe := filepath.Join(path, ".histex-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o666); err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// ensureDataDir is ensure_data_dir().
func ensureDataDir(path string) string {
	ensureDir(path)
	return path
}

// Config mirrors DEFAULT_CONFIG plus the two runtime keys the Python version
// kept in the same dict: "_datadir", "_path" and "_preview_off".
type Config struct {
	Recipes       *string  `json:"recipes"`
	ScriptsDir    *string  `json:"scripts_dir"`
	JSONL         *string  `json:"jsonl"`
	History       *string  `json:"history"`
	Shell         string   `json:"shell"`
	Network       bool     `json:"network"`
	Cache         bool     `json:"cache"`
	CacheTTLHours float64  `json:"cache_ttl_hours"`
	CacheDir      *string  `json:"cache_dir"`
	Sort          string   `json:"sort"`
	MaxItems      int      `json:"max_items"`
	Detail        string   `json:"detail"`
	ExplainOrder  []string `json:"explain_order"`
	Tldr          bool     `json:"tldr"`
	ToolHelp      bool     `json:"toolhelp"`
	PreviewWindow string   `json:"preview_window"`
	Exclude       []string `json:"exclude"`
	Secrets       []string `json:"secrets"`
	Danger        []string `json:"danger"`
	Expect        string   `json:"expect"`
	ClipKey       string   `json:"clip_key"`
	ReloadKey     string   `json:"reload_key"`
	Preview       bool     `json:"preview"`
	PreviewKey    string   `json:"preview_key"`
	Snippet       *string  `json:"snippet"`
	Sidecar       *string  `json:"sidecar"`

	DataDir    string `json:"-"`
	Path       string `json:"-"`
	PreviewOff bool   `json:"-"`
}

func defaultConfig() *Config {
	return &Config{
		Shell:         "auto",
		Network:       true,
		Cache:         true,
		CacheTTLHours: 168,
		Sort:          "recent",
		MaxItems:      5000,
		Detail:        "short",
		ExplainOrder:  []string{"cache", "local", "tldr", "toolhelp", "cheat"},
		Tldr:          true,
		ToolHelp:      true,
		PreviewWindow: "right:40%:wrap",
		Exclude: []string{
			`^(cls|clear|exit|quit)\s*$`,
			`^.{1,2}$`,
			`histex2?(\.py)?(\s|$)`,
		},
		Secrets: []string{
			"password", "passwd", "token", "secret", "api[_-]?key",
			"sshpass", `bearer\s`, "private[_-]?key",
		},
		Danger: []string{
			`Remove-Item.*-(Recurse|Force)`,
			`\brm\s+-[a-z]*r[a-z]*f`,
			`git\s+reset\s+--hard`,
			`Stop-Process.*-Force`,
			`Format-Volume`,
			`DROP\s+(TABLE|DATABASE)`,
		},
		Expect:     "ctrl-t,ctrl-x",
		ClipKey:    "ctrl-o",
		ReloadKey:  "ctrl-r",
		Preview:    false,
		PreviewKey: "ctrl-p",
	}
}

// dataFilenames is DATA_FILENAMES; the order matters because _fill_data_paths
// appends "snippet" and "sidecar" to the config in this order.
var dataFilenameOrder = []string{
	"recipes", "scripts_dir", "jsonl", "cache_dir", "snippet", "sidecar",
}

var dataFilenames = map[string]string{
	"recipes":     "saved_recipes.md",
	"scripts_dir": "scripts",
	"jsonl":       "recipes.jsonl",
	"cache_dir":   "cache",
	"snippet":     "histex_profile.ps1",
	"sidecar":     "history_log.tsv",
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrTo(value string) *string {
	return &value
}

// get returns one of the six path settings ("" when unset).
func (c *Config) get(key string) string {
	switch key {
	case "recipes":
		return deref(c.Recipes)
	case "scripts_dir":
		return deref(c.ScriptsDir)
	case "jsonl":
		return deref(c.JSONL)
	case "history":
		return deref(c.History)
	case "cache_dir":
		return deref(c.CacheDir)
	case "snippet":
		return deref(c.Snippet)
	case "sidecar":
		return deref(c.Sidecar)
	}
	return ""
}

func (c *Config) set(key string, value string) {
	switch key {
	case "recipes":
		c.Recipes = ptrTo(value)
	case "scripts_dir":
		c.ScriptsDir = ptrTo(value)
	case "jsonl":
		c.JSONL = ptrTo(value)
	case "history":
		c.History = ptrTo(value)
	case "cache_dir":
		c.CacheDir = ptrTo(value)
	case "snippet":
		c.Snippet = ptrTo(value)
	case "sidecar":
		c.Sidecar = ptrTo(value)
	}
}

// fillDataPaths is _fill_data_paths(): None values become <root>/<name>.
func fillDataPaths(cfg *Config) *Config {
	root := cfg.DataDir
	if root == "" {
		root = resolveDataDir("")
	}
	cfg.DataDir = root
	for _, key := range dataFilenameOrder {
		if cfg.get(key) == "" {
			cfg.set(key, filepath.Join(root, dataFilenames[key]))
		}
	}
	return cfg
}

// dataPath is data_path(): a resolved path for a data-dir setting.
func dataPath(cfg *Config, key string) string {
	if value := cfg.get(key); value != "" {
		return value
	}
	root := cfg.DataDir
	if root == "" {
		root = resolveDataDir("")
	}
	if key == "cache_dir" {
		return filepath.Join(root, "cache")
	}
	name := dataFilenames[key]
	if name == "" {
		name = key
	}
	return filepath.Join(root, name)
}

// sortStatePath is sort_state_path(): where recent <-> freq is remembered.
func sortStatePath(cfg *Config) string {
	root := cfg.DataDir
	if root == "" {
		root = filepath.Dir(cfg.Path)
		if cfg.Path == "" {
			root = legacyHomeDir
		}
	}
	return filepath.Join(root, "sort.state")
}

// loadConfig is load_config(): the config file over the built-in defaults.
// Migration runs separately (in main) so --self-test never moves user data.
func loadConfig(path string, datadir string) *Config {
	if datadir == "" {
		datadir = resolveDataDir(os.Getenv("HISTEX_DATA_DIR"))
	}
	cfg := defaultConfig()
	cfg.DataDir = datadir
	if path == "" {
		path = os.Getenv("HISTEX_CONFIG")
	}
	if path == "" {
		path = filepath.Join(datadir, "config.json")
	}
	if isFile(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			errLine("[!] ignoring %s: %s", path, err)
		} else {
			text := decodeText(raw, true)
			if err := json.Unmarshal([]byte(text), cfg); err != nil {
				errLine("[!] ignoring %s: %s", path, err)
			}
			warnUnknownConfigKeys(text)
		}
	}
	cfg.Path = path
	return fillDataPaths(cfg)
}

// configPairs lists every setting in the order --init-config writes it. It is
// also the set of keys a config file is allowed to contain.
func configPairs(cfg *Config) jobject {
	return jobject{
		{"recipes", cfg.Recipes},
		{"scripts_dir", cfg.ScriptsDir},
		{"jsonl", cfg.JSONL},
		{"history", cfg.History},
		{"shell", cfg.Shell},
		{"network", cfg.Network},
		{"cache", cfg.Cache},
		{"cache_ttl_hours", cfg.CacheTTLHours},
		{"cache_dir", cfg.CacheDir},
		{"sort", cfg.Sort},
		{"max_items", cfg.MaxItems},
		{"detail", cfg.Detail},
		{"explain_order", cfg.ExplainOrder},
		{"tldr", cfg.Tldr},
		{"toolhelp", cfg.ToolHelp},
		{"preview_window", cfg.PreviewWindow},
		{"exclude", cfg.Exclude},
		{"secrets", cfg.Secrets},
		{"danger", cfg.Danger},
		{"expect", cfg.Expect},
		{"clip_key", cfg.ClipKey},
		{"reload_key", cfg.ReloadKey},
		{"preview", cfg.Preview},
		{"preview_key", cfg.PreviewKey},
		{"snippet", cfg.Snippet},
		{"sidecar", cfg.Sidecar},
	}
}

// configJSON serialises the config exactly like json.dumps(indent=2), in the
// same key order, including the appended snippet/sidecar keys.
func configJSON(cfg *Config) string {
	return pyJSON(configPairs(cfg), "  ", 0) + "\n"
}

// knownConfigKeys is the set of settings histex understands, taken from the
// same object --init-config writes, so the two cannot disagree.
func knownConfigKeys() map[string]bool {
	known := map[string]bool{}
	for _, pair := range configPairs(defaultConfig()) {
		known[pair.key] = true
	}
	return known
}

// warnUnknownConfigKeys reports misspelled settings instead of dropping them
// silently. The values themselves are ignored either way, exactly as before.
func warnUnknownConfigKeys(text string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return
	}
	known := knownConfigKeys()
	unknown := make([]string, 0, len(raw))
	for key := range raw {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		errLine("[!] ignoring unknown config key %q", key)
	}
}

// writeDefaultConfig is write_default_config(): used by --init-config.
func writeDefaultConfig(path string, cfg *Config) string {
	if cfg == nil {
		cfg = defaultConfig()
	}
	if path == "" {
		path = filepath.Join(resolveDataDir(""), "config.json")
	}
	ensureDir(filepath.Dir(path))
	atomicWrite(path, configJSON(cfg))
	return path
}

// legacyDataSources is legacy_data_sources(): the one-time data move map.
// The roots are injectable so --self-test never touches real user data.
func legacyDataSources(scriptDirOverride string, homeDirOverride string) (string, []namePath) {
	if scriptDirOverride == "" {
		scriptDirOverride = scriptDir
	}
	if homeDirOverride == "" {
		homeDirOverride = legacyHomeDir
	}
	scripts := filepath.Join(scriptDirOverride, "scripts")
	pairs := []namePath{
		{"saved_recipes.md", filepath.Join(scriptDirOverride, "saved_recipes.md")},
		{"recipes.jsonl", filepath.Join(scriptDirOverride, "recipes.jsonl")},
		{"history_log.tsv", filepath.Join(scriptDirOverride, "history_log.tsv")},
		{"histex_profile.ps1", filepath.Join(scriptDirOverride, "histex_profile.ps1")},
		{"sort.state", filepath.Join(homeDirOverride, "sort.state")},
		{"config.json", filepath.Join(homeDirOverride, "config.json")},
	}
	return scripts, pairs
}

type namePath struct {
	name string
	path string
}

// migrateData is migrate_data(): copy-only, never overwrites, never deletes,
// idempotent, and it writes a migrated.json receipt.
func migrateData(datadir string, verbose bool, scriptDirOverride string, homeDirOverride string) (int, int) {
	moved, skipped := 0, 0
	ensureDataDir(datadir)
	scriptLegacy, pairs := legacyDataSources(scriptDirOverride, homeDirOverride)

	copyOne := func(name string, source string) int {
		target := filepath.Join(datadir, name)
		if !isFile(source) {
			return 0
		}
		if isFile(target) {
			if verbose {
				errLine("[i] already present, kept: %s", name)
			}
			return -1
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			errLine("[!] could not migrate %s: %s", source, err)
			return 0
		}
		if err := atomicWriteRaw(target, raw); err != nil {
			errLine("[!] could not migrate %s: %s", source, err)
			return 0
		}
		// stderr: migration must never pollute --json / --pick / --print-list
		errLine("[i] migrated %s -> %s", source, datadir)
		return 1
	}

	copyTree := func(name string, source string) int {
		target := filepath.Join(datadir, name)
		if !isDir(source) {
			return 0
		}
		movedFiles := 0
		if err := ensureDir(target); err != nil {
			errLine("[!] could not migrate %s: %s", source, err)
			return 0
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			errLine("[!] could not migrate %s: %s", source, err)
			return 0
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names) // Python used sorted(os.listdir(...))
		for _, entry := range names {
			origin := filepath.Join(source, entry)
			final := filepath.Join(target, entry)
			if !isFile(origin) || isFile(final) {
				continue
			}
			raw, err := os.ReadFile(origin)
			if err != nil {
				continue
			}
			if err := atomicWriteRaw(final, raw); err != nil {
				continue
			}
			movedFiles++
		}
		if movedFiles > 0 {
			errLine("[i] migrated %s (%d files) -> %s", source, movedFiles, target)
		}
		return movedFiles
	}

	for _, pair := range pairs {
		result := copyOne(pair.name, pair.path)
		if result > 0 {
			moved += result
		} else if result < 0 {
			skipped++
		}
	}
	moved += copyTree("scripts", scriptLegacy)
	cacheHome := homeDirOverride
	if cacheHome == "" {
		cacheHome = legacyHomeDir
	}
	moved += copyTree("cache", filepath.Join(cacheHome, "cache"))

	receipt := filepath.Join(datadir, "migrated.json")
	record := jobject{
		{"ts", time.Now().Format("2006-01-02 15:04:05")},
		{"moved", moved},
		{"skipped", skipped},
	}
	atomicWrite(receipt, pyJSON(record, "", 0)+"\n")
	return moved, skipped
}
