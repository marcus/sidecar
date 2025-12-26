package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sst/sidecar/internal/adapter"
	"github.com/sst/sidecar/internal/app"
	"github.com/sst/sidecar/internal/config"
	"github.com/sst/sidecar/internal/event"
	"github.com/sst/sidecar/internal/keymap"
	"github.com/sst/sidecar/internal/plugin"
)

var (
	configPath  = flag.String("config", "", "path to config file")
	projectRoot = flag.String("project", ".", "project root directory")
	debug       = flag.Bool("debug", false, "enable debug logging")
)

func main() {
	flag.Parse()

	// Setup logging
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create event dispatcher
	dispatcher := event.NewWithLogger(logger)
	defer dispatcher.Close()

	// Create plugin context
	pluginCtx := &plugin.Context{
		WorkDir:   *projectRoot,
		ConfigDir: config.ConfigPath(),
		Adapters:  make(map[string]adapter.Adapter),
		EventBus:  dispatcher,
		Logger:    logger,
	}

	// Create plugin registry
	registry := plugin.NewRegistry(pluginCtx)

	// TODO: Register plugins when they're implemented
	// registry.Register(gitstatus.New(cfg.Plugins.GitStatus))
	// registry.Register(tdmonitor.New(cfg.Plugins.TDMonitor))
	// registry.Register(conversations.New(cfg.Plugins.Conversations))

	// Create keymap registry
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	// Apply user keymap overrides
	for key, cmdID := range cfg.Keymap.Overrides {
		km.SetUserOverride(key, cmdID)
	}

	// Create and run application
	model := app.New(registry, km)
	p := tea.NewProgram(model, tea.WithAltScreen())

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
