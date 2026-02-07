# fixed footer layout

## overview
Sidecar renders a fixed app shell with:
- header: plugin tabs, title, clock
- content: plugin view area (scrollable within the plugin)
- footer: global + plugin-specific key hints and status

The footer is rendered by the app shell (`internal/app/view.go`) and is not part
of any plugin view.

## plugin responsibilities
Plugins only render their main content area. Do not render a footer or assume
extra terminal rows are available outside your content.

When implementing a plugin view:
- treat the `height` argument as the full usable content height
- keep your own header (optional) inside that height
- calculate visible rows based on your own header/spacing
- do not add footer rows or key hints

## key hints in the footer
The footer builds hints from:
- global bindings (tab switch, help, quit)
- active plugin commands (`Plugin.Commands()` + active context bindings)

To expose hints for a plugin:
1. Add command entries in `Plugin.Commands()` with the correct `Context`.
2. Ensure key bindings exist for those command IDs in `internal/keymap/bindings.go`
   (or user overrides).
3. Return the active context from `Plugin.FocusContext()` so the shell knows
   which bindings to use.

### example
```go
func (p *Plugin) Commands() []plugin.Command {
    return []plugin.Command{
        {ID: "open-item", Name: "Open", Context: "my-plugin", Priority: 1},
        {ID: "back", Name: "Back", Context: "my-plugin-detail", Priority: 1},
    }
}

func (p *Plugin) FocusContext() string {
    if p.showDetail {
        return "my-plugin-detail"
    }
    return "my-plugin"
}
```

Commands are shown in the footer in priority order (lower number = more important).
Use Priority 1 for the most essential commands, 2-4 for secondary actions, and skip
or use 5+ for rare actions. Default (0) is treated as 99 (lowest).

And ensure bindings exist:
```go
{Key: "enter", Command: "open-item", Context: "my-plugin"},
{Key: "esc", Command: "back", Context: "my-plugin-detail"},
```

### Context Naming
Use kebab-case with semantic names describing the view state:
- "plugin-name" for main view
- "plugin-name-detail" for detail/preview panes
- "plugin-name-modal" for modal overlays
- "plugin-name-search" for search modes

Examples from built-in plugins:
- git: "git-status", "git-status-commits", "git-diff", "git-commit"
- file-browser: "file-browser-tree", "file-browser-preview", "file-browser-search"

### Footer Behavior
The footer automatically truncates hints if they exceed available width. Limit
Command Names to 1-2 words to prevent truncation. Plugin hints appear before
global hints.

Global context bindings (tab switching, help, quit) are always available and
shown in the footer after plugin-specific hints. Don't redefine global commands
in your plugin.

## layout math guidelines
Inside a plugin view, compute the number of visible rows by subtracting only
the rows you render yourself (headers, section titles, blank lines).

Do not subtract a footer height or assume the terminal height includes extra
space beyond the content area provided by the app shell.
