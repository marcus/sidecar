package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/marcus/sidecar/internal/adapter/amp"
	_ "github.com/marcus/sidecar/internal/adapter/antigravity"
	_ "github.com/marcus/sidecar/internal/adapter/claudecode"
	_ "github.com/marcus/sidecar/internal/adapter/codex"
	_ "github.com/marcus/sidecar/internal/adapter/copilot"
	_ "github.com/marcus/sidecar/internal/adapter/cursor"
	_ "github.com/marcus/sidecar/internal/adapter/grok"
	_ "github.com/marcus/sidecar/internal/adapter/kiro"
	_ "github.com/marcus/sidecar/internal/adapter/muse"
	_ "github.com/marcus/sidecar/internal/adapter/omp"
	_ "github.com/marcus/sidecar/internal/adapter/opencode"
	_ "github.com/marcus/sidecar/internal/adapter/pi"
	_ "github.com/marcus/sidecar/internal/adapter/piagent"
	_ "github.com/marcus/sidecar/internal/adapter/warp"
	"github.com/marcus/sidecar/internal/agentcatalog"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/buildinfo"
	"github.com/marcus/sidecar/internal/cli"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/configui"
	"github.com/marcus/sidecar/internal/event"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/assembly"
	"github.com/marcus/sidecar/internal/projectdir"
	"github.com/marcus/sidecar/internal/shellstate"
	"github.com/marcus/sidecar/internal/startupfail"
	"github.com/marcus/sidecar/internal/startuptrace"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/terminalperf"
	"github.com/marcus/sidecar/internal/termtitle"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/tmuxenv"
	"github.com/marcus/sidecar/internal/tty"
	"golang.org/x/term"
)

// Build metadata injected at build time via ldflags. Version is the only field
// both the managed install and the release pipeline set; the rest fall back to
// debug.ReadBuildInfo, which carries VCS data for an ordinary `go build`.
var (
	Version      = "" // release version, or the composite devel+branch.sha from dev-install.sh
	Commit       = "" // short commit hash
	Dirty        = "" // "true" when the tree was modified at build time
	BuildDate    = "" // RFC3339 build timestamp
	BuildProfile = "" // "release" for goreleaser builds, "development" otherwise
)

var (
	configPath     = flag.String("config", "", "path to config file")
	projectRoot    = flag.String("project", ".", "project root directory")
	debugFlag      = flag.Bool("debug", false, "enable debug logging")
	versionFlag    = flag.Bool("version", false, "print version and exit")
	shortVersion   = flag.Bool("v", false, "print version and exit (short)")
	enableFeature  = flag.String("enable-feature", "", "enable a feature flag (comma-separated)")
	disableFeature = flag.String("disable-feature", "", "disable a feature flag (comma-separated)")
)

func main() {
	// Publish the ldflags-injected version to library code before anything can
	// read it. `sidecar host serve` puts this string in its protocol hello, and
	// that hello is built under internal/, which cannot see package main.
	buildinfo.Set(Version)

	// Non-interactive commands dispatch before flag parsing and before any TUI
	// initialization, logging, state creation, or TMUX environment changes.
	args := os.Args[1:]
	if handled, code := cli.Run(args, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	// A launch command — `sidecar setup` — is not handled here: it records the
	// destination the app should open on and leaves the rest of the arguments
	// for ordinary flag parsing, so `sidecar setup -project /x` behaves like
	// `sidecar -project /x` that happens to open Configuration.
	_ = flag.CommandLine.Parse(cli.RemainingArgs(args))

	// Record -config before anything derives a path from it: the config
	// directory is also where debug.log and state.json live, so pointing the
	// flag at a temp dir moves the whole config axis (td-8d18de).
	config.SetConfigPath(*configPath)

	// Fail closed before anything touches the filesystem. This has to precede
	// openLogFile, which creates the config directory and appends to debug.log:
	// a run that claims isolation must not have already written the real user
	// tree by the time it refuses to run (td-8d18de). CheckStateIsolation needs
	// nothing from the config file — only the resolved state and config paths.
	if err := config.CheckStateIsolation(); err != nil {
		startupfail.Print(os.Stderr, startupfail.Isolation(err))
		os.Exit(1)
	}

	// Unset TMUX so sidecar's internal tmux sessions are independent of any
	// outer tmux session. This allows prefix+d to detach from the workspace's
	// inner session rather than the user's outer tmux.
	_ = os.Unsetenv("TMUX")
	// After TMUX is unset, tmux children talk to this process's default
	// socket — the server that holds Sidecar-managed sessions. Tests never
	// reach here, so they cannot query the developer's live server.
	tty.SessionOwner = tty.ReadTmuxSessionOwner

	// The performance snapshot shares pprof's localhost-only diagnostic server.
	// Keep it opt-in so ordinary builds pay only the existing nil-probe check.
	if terminalPerformanceEnabled(os.Getenv("SIDECAR_TERMINAL_PERF")) {
		counters := &terminalperf.Counters{}
		restore := terminalperf.Install(counters)
		defer restore()
		http.Handle(terminalperf.SnapshotPath, terminalperf.SnapshotHandler(counters))
	}

	// Start pprof server if enabled (for memory profiling)
	if pprofPort := os.Getenv("SIDECAR_PPROF"); pprofPort != "" {
		if pprofPort == "1" {
			pprofPort = "6060" // default port
		}
		go func() {
			addr := "localhost:" + pprofPort
			fmt.Fprintf(os.Stderr, "pprof enabled on http://%s/debug/pprof/\n", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				fmt.Fprintf(os.Stderr, "pprof server error: %v\n", err)
			}
		}()
	}

	// Handle version flag
	if *versionFlag || *shortVersion {
		for _, line := range versionLines(effectiveVersion(Version), resolveBuildDetails()) {
			fmt.Println(line)
		}
		os.Exit(0)
	}

	// Setup logging to file (never to stderr - it leaks through TUI)
	logLevel := slog.LevelInfo
	if *debugFlag {
		logLevel = slog.LevelDebug
	}
	logFile, err := openLogFile()
	if err != nil {
		// Fall back to discarding logs if we can't open file
		logFile = nil
	}
	var logWriter = io.Discard
	if logFile != nil {
		logWriter = logFile
		defer func() {
			if err := logFile.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to close log file: %v\n", err)
			}
		}()
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Load configuration
	var cfg *config.Config
	startuptrace.Track("config.Load", func() {
		cfg, err = loadConfig(*configPath)
	})
	if err != nil {
		startupfail.Print(os.Stderr, startupfail.ConfigLoad(config.ConfigPath(), err))
		os.Exit(1)
	}

	// Fold the user's agent catalog overlay in before anything reads the
	// catalog: the creation pickers, Configuration and the workspace plugin all
	// read it, and a family added here has to be there before the first of them
	// asks. It is filesystem work, so it is here on the startup path rather
	// than in a plugin's Init, and a malformed file is a warning rather than a
	// refusal to start.
	startuptrace.Track("agentcatalog.LoadOverlay", func() {
		for _, problem := range agentcatalog.LoadOverlay(config.AgentCatalogDir()) {
			logger.Warn("agent catalog overlay file skipped", "error", problem)
		}
	})

	// Which provider commands are installed is what the creation pickers filter
	// on. It is one PATH walk per family, so it runs once, here, off every
	// render path, and in the background: nothing on the first frame needs the
	// answer, and a picker that opens before it lands offers everything rather
	// than nothing.
	go agentcatalog.PrimeInstalled()

	// Initialize feature flags
	features.Init(cfg)
	applyFeatureOverrides()

	// Hand shellstate the retention window from the config we just read, so no
	// manifest write has to re-read config.json to learn it — one of those
	// writes is on the startup path.
	shellstate.SetTombstoneRetention(cfg.Shells.TombstoneRetentionWindow())

	// Load persistent state (ignore errors - state is optional)
	// state.json lives next to config.json, so -config relocates it too.
	// With no flag this is ~/.config/sidecar, identical to state.Init().
	startuptrace.Track("state.Init", func() {
		_ = state.InitWithDir(filepath.Dir(config.ConfigPath()))
	})

	// Create event dispatcher
	dispatcher := event.NewWithLogger(logger)
	defer dispatcher.Close()

	// Convert project root to absolute path
	workDir, err := filepath.Abs(*projectRoot)
	if err != nil {
		startupfail.Print(os.Stderr, startupfail.ProjectRoot(*projectRoot, err))
		os.Exit(1)
	}

	// Resolve project root (main worktree for linked worktrees, same as workDir otherwise)
	var projectRootPath string
	startuptrace.Track("app.GetMainWorktreePath", func() {
		projectRootPath = app.GetMainWorktreePath(workDir)
	})
	if projectRootPath == "" {
		projectRootPath = workDir
	}

	logResolvedPaths(logger, projectRootPath)

	// Apply theme from config (after workDir is known for per-project themes)
	startuptrace.Track("theme.Resolve+Apply", func() {
		resolved := theme.ResolveTheme(cfg, workDir)
		theme.ApplyResolved(resolved)
	})

	// Apply UI settings (Nerd Font features)
	styles.PillTabsEnabled = cfg.UI.NerdFontsEnabled

	// Create keymap registry first (plugins may register bindings during Init)
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	// Create plugin context with keymap for dynamic binding registration
	pluginCtx := &plugin.Context{
		WorkDir:     workDir,
		ProjectRoot: projectRootPath,
		ConfigDir:   config.ConfigPath(),
		Config:      cfg,
		Adapters:    make(map[string]adapter.Adapter),
		EventBus:    dispatcher,
		Logger:      logger,
		Keymap:      km,
	}

	// History adapters exist only for the Conversations plugin. Skip
	// construction entirely when the plugin is unwanted so Detect/session I/O
	// and adapter state never run (conversations_plugin feature flag, default off).
	if assembly.ConversationsWanted(cfg) {
		// Create all adapter instances upfront so they survive project switches.
		// Per-project filtering happens in each plugin's Init() via Detect().
		startuptrace.Track("adapter.AllAdapters", func() {
			pluginCtx.Adapters = adapter.AllAdapters()
		})
	}

	// Create plugin registry
	registry := plugin.NewRegistry(pluginCtx)

	// Register plugins. Order (and therefore tab order and the derived tab
	// shortcut numbers) is owned by internal/plugins/assembly.
	assembly.Register(registry, cfg, logger)

	// Apply user keymap overrides
	for key, cmdID := range cfg.Keymap.Overrides {
		km.SetUserOverride(key, cmdID)
	}

	// Create and run application
	currentVersion := effectiveVersion(Version)
	initialPluginID := initialPluginForWorkDir(workDir, projectRootPath)
	// The settings page renders the plugin catalog; assembly owns it, and the
	// app shell cannot import assembly, so it is handed over here.
	options := []app.Option{app.WithPluginDescriptors(assembly.Descriptors())}
	if page, ok := cli.StartupConfigPage(); ok {
		// The only caller today is `sidecar setup`. Configuration opens on its
		// default page unless a caller deliberately named another one.
		options = append(options, app.WithStartupConfigPage(configui.PageID(page)))
	}
	var model app.Model
	startuptrace.Track("app.New", func() {
		model = app.New(registry, km, cfg, currentVersion, workDir, projectRootPath, initialPluginID, options...)
	})

	// Guard against non-interactive terminal (e.g. piped stdout)
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		startupfail.Print(os.Stderr, startupfail.NotATerminal())
		os.Exit(1)
	}
	// Bubble Tea clears the window title on exit rather than restoring it, so
	// bracket the run with the terminal's title stack. Terminals that don't
	// implement the stack ignore both sequences and are left where they would
	// have been anyway: window title blank, icon name at sidecar's last value.
	restoreTitle := func() {}
	if cfg.UI.TerminalTitle != "" {
		fmt.Print(termtitle.Save())
		restoreTitle = func() { fmt.Print(termtitle.Restore()) }
	}

	// v2: terminal features (alt-screen, mouse) are declared on tea.View in
	// the app's View() method, not as NewProgram options.
	p := tea.NewProgram(model, tea.WithFilter(app.FilterInput))

	startuptrace.Mark("tea.Program.Run")
	if startuptrace.Enabled() {
		// Report on exit, and again a few seconds in so the trace is usable
		// without having to quit the app cleanly.
		defer startuptrace.Report(logger)
		time.AfterFunc(startuptrace.ReportDelay(), func() { startuptrace.Report(logger) })
	}
	// Provider work and the cold-restore pass are the things the app starts that
	// outlive a frame and own child processes, so they get an explicit stop on
	// both exit paths. A restore cancelled mid-run leaves every record intact.
	defer app.ShutdownResourceProviders()
	defer app.ShutdownSessionRestore()
	if _, err := p.Run(); err != nil {
		app.ShutdownResourceProviders()
		app.ShutdownSessionRestore()
		// Report before exiting: os.Exit skips deferred calls, and a trace of a
		// run that died is exactly the one worth having.
		startuptrace.Report(logger)
		tty.ReleaseGeometryLeases()
		restoreTitle()
		startupfail.Print(os.Stderr, startupfail.Terminal(err))
		os.Exit(1)
	}
	// Hand the shared tmux geometry lease back so the next sidecar — here or on
	// another machine — is not waiting out a lease nobody holds (td-ee222a).
	tty.ReleaseGeometryLeases()
	restoreTitle()
}

func terminalPerformanceEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// initialPluginForWorkDir keeps process startup on the same per-worktree state
// path as in-app switching. The accessor also performs the additive migration
// from legacy repository-root keys without deleting the rollback entry.
func initialPluginForWorkDir(workDir, projectRootPath string) string {
	return state.GetActivePluginForWorkDir(workDir, projectRootPath)
}

func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.LoadFrom(path)
	}
	return config.Load()
}

// effectiveVersion returns the version string this binary reports.
//
// It delegates rather than deriving. The same string goes into `sidecar host
// serve`'s protocol hello, which is built under internal/ and cannot see this
// package — and two independently maintained copies of the derivation would
// eventually disagree about the version of the very binary the hello is
// describing.
func effectiveVersion(v string) string {
	buildinfo.Set(v)
	return buildinfo.Version()
}

// buildDetails is the build metadata reported under the first line of --version.
type buildDetails struct {
	commit  string
	dirty   bool
	date    string
	profile string
}

// resolveBuildDetails prefers the ldflags values and falls back to
// debug.ReadBuildInfo, so a plain `go build` still reports a usable commit.
func resolveBuildDetails() buildDetails {
	details := buildDetails{
		commit:  Commit,
		dirty:   Dirty == "true",
		date:    BuildDate,
		profile: BuildProfile,
	}

	if details.commit == "" || details.date == "" || Dirty == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if details.commit == "" {
						details.commit = shortCommit(setting.Value)
					}
				case "vcs.modified":
					if Dirty == "" {
						details.dirty = setting.Value == "true"
					}
				case "vcs.time":
					if details.date == "" {
						details.date = setting.Value
					}
				}
			}
		}
	}

	if details.profile == "" {
		details.profile = "development"
	}

	return details
}

// shortCommit truncates a revision to the customary seven characters.
func shortCommit(revision string) string {
	if len(revision) > 7 {
		return revision[:7]
	}
	return revision
}

// versionLines renders the --version output. The first line is load-bearing and
// must stay a single "sidecar version <v>" line: scripts/dev-install.sh matches
// it by prefix and scripts/verify-release-archives.sh compares it exactly. Any
// added detail belongs on the indented lines below it.
func versionLines(version string, details buildDetails) []string {
	lines := []string{"sidecar version " + version}

	if details.commit != "" {
		commit := details.commit
		if details.dirty {
			commit += " (dirty)"
		}
		lines = append(lines, "  commit:  "+commit)
	}
	if details.date != "" {
		lines = append(lines, "  date:    "+details.date)
	}
	lines = append(lines, "  profile: "+details.profile)

	return lines
}

func init() {
	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sidecar [options] [command]\n\n")
		fmt.Fprintf(os.Stderr, "A TUI dashboard and command-line companion for AI coding agents.\n\n")
		// Listed from the command registry, so this stays true when a command
		// is added there rather than becoming a second list to remember.
		fmt.Fprintf(os.Stderr, "Commands:\n")
		for _, cmd := range cli.RootCommand().Sub {
			fmt.Fprintf(os.Stderr, "  %-14s %s\n", cmd.Name, cmd.Summary)
		}
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
}

// openLogFile creates/opens the debug log file in config directory.
func openLogFile() (*os.File, error) {
	dir := filepath.Dir(config.ConfigPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "debug.log")
	return os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

// logResolvedPaths records, in one line, every root this process will write to.
// Proof runs need it to confirm at a glance that both isolation axes actually
// moved — tmux transport and Sidecar state are separate, and isolating only one
// is what let a test overwrite a live shell manifest (td-8d18de).
func logResolvedPaths(logger *slog.Logger, projectRootPath string) {
	// Lookup, not Resolve: reporting a path must not create it.
	manifest := "<not yet created>"
	if dir, ok := projectdir.Lookup(projectRootPath); ok {
		manifest = filepath.Join(dir, "shells.json")
	}

	line := fmt.Sprintf("sidecar paths: state=%s config=%s tmux-socket=%s project-root=%s manifest=%s",
		config.StateDir(), config.ConfigPath(), tmuxenv.SocketPath(), projectRootPath, manifest)
	logger.Info(line)

	// Stderr is only safe here because bubbletea has not taken the screen yet.
	// Exactly one line, and only when someone asked for it.
	if os.Getenv("SIDECAR_DIAG_PATHS") == "1" || config.IsolationAsserted() {
		fmt.Fprintln(os.Stderr, line)
	}
}

// applyFeatureOverrides applies CLI feature flag overrides.
func applyFeatureOverrides() {
	if *enableFeature != "" {
		for _, name := range strings.Split(*enableFeature, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !features.IsKnownFeature(name) {
				fmt.Fprintf(os.Stderr, "warning: unknown feature '%s'\n", name)
			}
			features.SetOverride(name, true)
		}
	}
	if *disableFeature != "" {
		for _, name := range strings.Split(*disableFeature, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !features.IsKnownFeature(name) {
				fmt.Fprintf(os.Stderr, "warning: unknown feature '%s'\n", name)
			}
			features.SetOverride(name, false)
		}
	}
}
