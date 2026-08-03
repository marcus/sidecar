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
	_ "github.com/marcus/sidecar/internal/adapter/claudecode"
	_ "github.com/marcus/sidecar/internal/adapter/codex"
	_ "github.com/marcus/sidecar/internal/adapter/copilot"
	_ "github.com/marcus/sidecar/internal/adapter/cursor"
	_ "github.com/marcus/sidecar/internal/adapter/geminicli"
	_ "github.com/marcus/sidecar/internal/adapter/goose"
	_ "github.com/marcus/sidecar/internal/adapter/kiro"
	_ "github.com/marcus/sidecar/internal/adapter/omp"
	_ "github.com/marcus/sidecar/internal/adapter/opencode"
	_ "github.com/marcus/sidecar/internal/adapter/pi"
	_ "github.com/marcus/sidecar/internal/adapter/piagent"
	_ "github.com/marcus/sidecar/internal/adapter/warp"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/event"
	"github.com/marcus/sidecar/internal/features"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/conversations"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/plugins/notes"
	"github.com/marcus/sidecar/internal/plugins/tdmonitor"
	"github.com/marcus/sidecar/internal/plugins/workspace"
	"github.com/marcus/sidecar/internal/startuptrace"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
	"github.com/marcus/sidecar/internal/termtitle"
	"github.com/marcus/sidecar/internal/theme"
	"github.com/marcus/sidecar/internal/tty"
	"golang.org/x/term"
)

// Version is set at build time via ldflags
var Version = ""

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
	flag.Parse()

	// Unset TMUX so sidecar's internal tmux sessions are independent of any
	// outer tmux session. This allows prefix+d to detach from the workspace's
	// inner session rather than the user's outer tmux.
	_ = os.Unsetenv("TMUX")

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
		fmt.Printf("sidecar version %s\n", effectiveVersion(Version))
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
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize feature flags
	features.Init(cfg)
	applyFeatureOverrides()

	// Load persistent state (ignore errors - state is optional)
	startuptrace.Track("state.Init", func() { _ = state.Init() })

	// Create event dispatcher
	dispatcher := event.NewWithLogger(logger)
	defer dispatcher.Close()

	// Convert project root to absolute path
	workDir, err := filepath.Abs(*projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve project root: %v\n", err)
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

	// Create all adapter instances upfront so they survive project switches.
	// Per-project filtering happens in each plugin's Init() via Detect().
	startuptrace.Track("adapter.AllAdapters", func() {
		pluginCtx.Adapters = adapter.AllAdapters()
	})

	// Create plugin registry
	registry := plugin.NewRegistry(pluginCtx)

	// Register plugins (order determines tab order)
	// TD plugin registers its bindings dynamically via p.ctx.Keymap
	if cfg.Plugins.TDMonitor.Enabled {
		if err := registry.Register(tdmonitor.New()); err != nil {
			logger.Warn("failed to register tdmonitor plugin", "err", err)
		}
	}
	if cfg.Plugins.GitStatus.Enabled {
		if err := registry.Register(gitstatus.New()); err != nil {
			logger.Warn("failed to register gitstatus plugin", "err", err)
		}
	}
	if cfg.Plugins.FileBrowser.Enabled {
		if err := registry.Register(filebrowser.New()); err != nil {
			logger.Warn("failed to register filebrowser plugin", "err", err)
		}
	}
	if cfg.Plugins.Conversations.Enabled {
		if err := registry.Register(conversations.New()); err != nil {
			logger.Warn("failed to register conversations plugin", "err", err)
		}
	}
	if err := registry.Register(workspace.New()); err != nil {
		logger.Warn("failed to register workspace plugin", "err", err)
	}
	if features.IsEnabled("notes_plugin") {
		if err := registry.Register(notes.New()); err != nil {
			logger.Warn("failed to register notes plugin", "err", err)
		}
	}

	// Apply user keymap overrides
	for key, cmdID := range cfg.Keymap.Overrides {
		km.SetUserOverride(key, cmdID)
	}

	// Create and run application
	currentVersion := effectiveVersion(Version)
	initialPluginID := state.GetActivePlugin(projectRootPath)
	var model app.Model
	startuptrace.Track("app.New", func() {
		model = app.New(registry, km, cfg, currentVersion, workDir, projectRootPath, initialPluginID)
	})

	// Guard against non-interactive terminal (e.g. piped stdout)
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "sidecar requires an interactive terminal")
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
	p := tea.NewProgram(model)

	startuptrace.Mark("tea.Program.Run")
	if startuptrace.Enabled() {
		// Report on exit, and again a few seconds in so the trace is usable
		// without having to quit the app cleanly.
		defer startuptrace.Report(logger)
		time.AfterFunc(startuptrace.ReportDelay(), func() { startuptrace.Report(logger) })
	}
	if _, err := p.Run(); err != nil {
		// Report before exiting: os.Exit skips deferred calls, and a trace of a
		// run that died is exactly the one worth having.
		startuptrace.Report(logger)
		tty.ReleaseGeometryLeases()
		restoreTitle()
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		os.Exit(1)
	}
	// Hand the shared tmux geometry lease back so the next sidecar — here or on
	// another machine — is not waiting out a lease nobody holds (td-ee222a).
	tty.ReleaseGeometryLeases()
	restoreTitle()
}

func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.LoadFrom(path)
	}
	return config.Load()
}

// effectiveVersion returns the version string, with fallback to build info.
func effectiveVersion(v string) string {
	if v != "" {
		return v
	}

	// Try to get version from Go build info
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	// Check module version
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Fall back to VCS info
	var revision string
	var dirty bool

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}

	if revision != "" {
		ver := "devel+" + revision
		if len(ver) > 20 {
			ver = ver[:20]
		}
		if dirty {
			ver += "+dirty"
		}
		return ver
	}

	return "devel"
}

func init() {
	// Customize usage output
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sidecar [options]\n\n")
		fmt.Fprintf(os.Stderr, "A TUI dashboard for AI coding agents.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
}

// openLogFile creates/opens the debug log file in config directory.
func openLogFile() (*os.File, error) {
	logPath := filepath.Join(filepath.Dir(config.ConfigPath()), "debug.log")
	return os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
