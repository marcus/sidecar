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
        {ID: "open-item", Name: "Open", Context: "my-plugin"},
        {ID: "back", Name: "Back", Context: "my-plugin-detail"},
    }
}

func (p *Plugin) FocusContext() string {
    if p.showDetail {
        return "my-plugin-detail"
    }
    return "my-plugin"
}
```

And ensure bindings exist:
```go
{Key: "enter", Command: "open-item", Context: "my-plugin"},
{Key: "esc", Command: "back", Context: "my-plugin-detail"},
```

## layout math guidelines
Inside a plugin view, compute the number of visible rows by subtracting only
the rows you render yourself (headers, section titles, blank lines).

Do not subtract a footer height or assume the terminal height includes extra
space beyond the content area provided by the app shell.
