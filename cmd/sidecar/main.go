package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/adapter"
	_ "github.com/marcus/sidecar/internal/adapter/claudecode"
	_ "github.com/marcus/sidecar/internal/adapter/codex"
	_ "github.com/marcus/sidecar/internal/adapter/cursor"
	_ "github.com/marcus/sidecar/internal/adapter/geminicli"
	_ "github.com/marcus/sidecar/internal/adapter/opencode"
	_ "github.com/marcus/sidecar/internal/adapter/warp"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/config"
	"github.com/marcus/sidecar/internal/event"
	"github.com/marcus/sidecar/internal/keymap"
	"github.com/marcus/sidecar/internal/plugin"
	"github.com/marcus/sidecar/internal/plugins/conversations"
	"github.com/marcus/sidecar/internal/plugins/filebrowser"
	"github.com/marcus/sidecar/internal/plugins/gitstatus"
	"github.com/marcus/sidecar/internal/plugins/tdmonitor"
	"github.com/marcus/sidecar/internal/state"
	"github.com/marcus/sidecar/internal/styles"
)

// Version is set at build time via ldflags
var Version = ""

var (
	configPath   = flag.String("config", "", "path to config file")
	projectRoot  = flag.String("project", ".", "project root directory")
	debugFlag    = flag.Bool("debug", false, "enable debug logging")
	versionFlag  = flag.Bool("version", false, "print version and exit")
	shortVersion = flag.Bool("v", false, "print version and exit (short)")
)

func main() {
	flag.Parse()

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
		defer logFile.Close()
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Apply theme from config
	styles.ApplyThemeWithOverrides(cfg.UI.Theme.Name, cfg.UI.Theme.Overrides)

	// Load persistent state (ignore errors - state is optional)
	_ = state.Init()

	// Create event dispatcher
	dispatcher := event.NewWithLogger(logger)
	defer dispatcher.Close()

	// Convert project root to absolute path
	workDir, err := filepath.Abs(*projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve project root: %v\n", err)
		os.Exit(1)
	}

	// Create keymap registry first (plugins may register bindings during Init)
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	// Create plugin context with keymap for dynamic binding registration
	pluginCtx := &plugin.Context{
		WorkDir:   workDir,
		ConfigDir: config.ConfigPath(),
		Adapters:  make(map[string]adapter.Adapter),
		EventBus:  dispatcher,
		Logger:    logger,
		Keymap:    km,
	}

	// Detect adapters
	adapters, err := adapter.DetectAdapters(workDir)
	if err != nil {
		logger.Warn("adapter detection failed", "err", err)
	}
	if len(adapters) > 0 {
		pluginCtx.Adapters = adapters
	}

	// Create plugin registry
	registry := plugin.NewRegistry(pluginCtx)

	// Register plugins (order determines tab order)
	// TD plugin registers its bindings dynamically via p.ctx.Keymap
	registry.Register(tdmonitor.New())
	registry.Register(gitstatus.New())
	registry.Register(filebrowser.New())
	registry.Register(conversations.New())

	// Apply user keymap overrides
	for key, cmdID := range cfg.Keymap.Overrides {
		km.SetUserOverride(key, cmdID)
	}

	// Create and run application
	currentVersion := effectiveVersion(Version)
	initialPluginID := state.GetActivePlugin(workDir)
	model := app.New(registry, km, currentVersion, workDir, initialPluginID)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		os.Exit(1)
	}
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

// getShortRevision returns the first 12 chars of a revision.
func getShortRevision(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
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
	logPath := filepath.Join(config.ConfigPath(), "debug.log")
	return os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

// Ensure strings import is used
var _ = strings.TrimSpace
