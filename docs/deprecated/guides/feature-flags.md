# Feature Flags Guide

This guide explains how to use and create feature flags in Sidecar.

## Overview

Feature flags allow experimental functionality to be gated behind user-configurable settings. This enables:
- Safe rollout of experimental features (default off)
- User opt-in for new functionality
- Easy rollback if issues occur

## Using Feature Flags

### Config File

Enable features in `~/.config/sidecar/config.json`:

```json
{
  "features": {
    "flags": {
      "tmux_interactive_input": true
    }
  }
}
```

### CLI Override

Override config settings at runtime:

```bash
# Enable a feature
sidecar --enable-feature=tmux_interactive_input

# Disable a feature
sidecar --disable-feature=tmux_interactive_input

# Multiple features (comma-separated)
sidecar --enable-feature=feature1,feature2
```

CLI overrides take precedence over config file settings.

Note: Using an unknown feature name in a CLI flag produces a warning but doesn't prevent the application from starting.

## Available Features

| Feature | Default | Description |
|---------|---------|-------------|
| `tmux_interactive_input` | true | Enable write support for tmux panes |
| `tmux_inline_edit` | true | Enable inline file editing via tmux in the files plugin |
| `notes_plugin` | false | Enable the notes plugin for capturing quick notes |

## For Developers

### Checking Feature State

```go
import "github.com/marcus/sidecar/internal/features"

if features.IsEnabled("tmux_interactive_input") {
    // Feature-gated code
}
```

### Adding a New Feature

1. Define the feature in `internal/features/features.go`:

```go
var MyNewFeature = Feature{
    Name:        "my_new_feature",
    Default:     false,
    Description: "Description of what this enables",
}
```

2. Add to the `allFeatures` slice:

```go
var allFeatures = []Feature{
    TmuxInteractiveInput,
    MyNewFeature, // Add here
}
```

3. Use the feature check in your code:

```go
if features.IsEnabled("my_new_feature") {
    // New functionality
}
```

### API Reference

```go
// Check if a feature is enabled
features.IsEnabled(name string) bool

// List all features with current state
features.List() map[string]bool

// List all features with metadata
features.ListAll() []Feature

// Set a feature in config (persists to disk)
features.SetEnabled(name string, enabled bool) error

// Set a runtime override (does not persist)
features.SetOverride(name string, enabled bool)

// Check if a feature name is registered (exists in codebase)
features.IsKnownFeature(name string) bool
```

### Priority Order

Feature state is resolved in this order (first match wins):
1. CLI override (`--enable-feature`, `--disable-feature`)
2. Config file (`features.flags` in config.json)
3. Default value (defined in code)

## Best Practices

### Naming Conventions
- Use snake_case for feature names (e.g., `my_new_feature`, not `myNewFeature`)
- Use descriptive names that indicate what functionality is being gated

### General Guidelines
- New experimental features should default to `false`
- Provide clear descriptions for each feature
- Document features in this guide when adding them
- Remove feature flags once features are stable
