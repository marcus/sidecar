package plugin

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mockPlugin implements Plugin for testing.
type mockPlugin struct {
	id         string
	initErr    error
	initPanic  bool
	startPanic bool
	stopPanic  bool
	started    bool
	stopped    bool
}

func (m *mockPlugin) ID() string                           { return m.id }
func (m *mockPlugin) Name() string                         { return m.id }
func (m *mockPlugin) Icon() string                         { return "📦" }
func (m *mockPlugin) IsFocused() bool                      { return false }
func (m *mockPlugin) SetFocused(bool)                      {}
func (m *mockPlugin) Commands() []Command                  { return nil }
func (m *mockPlugin) FocusContext() string                 { return m.id }
func (m *mockPlugin) View(w, h int) string                 { return "" }
func (m *mockPlugin) Update(msg tea.Msg) (Plugin, tea.Cmd) { return m, nil }

func (m *mockPlugin) Init(ctx *Context) error {
	if m.initPanic {
		panic("init panic")
	}
	return m.initErr
}

func (m *mockPlugin) Start() tea.Cmd {
	if m.startPanic {
		panic("start panic")
	}
	m.started = true
	return nil
}

func (m *mockPlugin) Stop() {
	if m.stopPanic {
		panic("stop panic")
	}
	m.stopped = true
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockPlugin{id: "test"}
	if err := r.Register(p); err != nil {
		t.Errorf("Register failed: %v", err)
	}

	plugins := r.Plugins()
	if len(plugins) != 1 {
		t.Errorf("got %d plugins, want 1", len(plugins))
	}
}

func TestRegistry_RegisterInitError(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockPlugin{id: "failing", initErr: errors.New("init failed")}
	if err := r.Register(p); err != nil {
		t.Errorf("Register should not return error for failed init: %v", err)
	}

	// Plugin should be in unavailable, not in active plugins
	plugins := r.Plugins()
	if len(plugins) != 0 {
		t.Errorf("got %d plugins, want 0", len(plugins))
	}

	unavail := r.Unavailable()
	if _, ok := unavail["failing"]; !ok {
		t.Error("plugin should be in unavailable map")
	}
}

func TestRegistry_RegisterInitPanic(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockPlugin{id: "panicky", initPanic: true}
	if err := r.Register(p); err != nil {
		t.Errorf("Register should not return error for panic: %v", err)
	}

	unavail := r.Unavailable()
	if _, ok := unavail["panicky"]; !ok {
		t.Error("panicked plugin should be in unavailable map")
	}
}

func TestRegistry_StartStop(t *testing.T) {
	r := NewRegistry(nil)

	p1 := &mockPlugin{id: "p1"}
	p2 := &mockPlugin{id: "p2"}
	_ = r.Register(p1)
	_ = r.Register(p2)

	r.Start()
	if !p1.started || !p2.started {
		t.Error("plugins should be started")
	}

	r.Stop()
	if !p1.stopped || !p2.stopped {
		t.Error("plugins should be stopped")
	}
}

func TestRegistry_StartStopPanic(t *testing.T) {
	r := NewRegistry(nil)

	p1 := &mockPlugin{id: "p1", startPanic: true}
	p2 := &mockPlugin{id: "p2", stopPanic: true}
	_ = r.Register(p1)
	_ = r.Register(p2)

	// Should not panic
	r.Start()
	r.Stop()
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry(nil)

	p := &mockPlugin{id: "findme"}
	_ = r.Register(p)

	found := r.Get("findme")
	if found == nil {
		t.Error("Get should find registered plugin")
	}

	notFound := r.Get("missing")
	if notFound != nil {
		t.Error("Get should return nil for missing plugin")
	}
}

// mockPluginWithInit tracks Init calls for testing Reinit.
type mockPluginWithInit struct {
	mockPlugin
	initCalls int
	lastCtx   *Context
}

func (m *mockPluginWithInit) Init(ctx *Context) error {
	m.initCalls++
	m.lastCtx = ctx
	return m.mockPlugin.Init(ctx)
}

func (m *mockPluginWithInit) Start() tea.Cmd {
	m.started = true
	// Return a non-nil command for testing
	return func() tea.Msg { return nil }
}

func TestRegistry_Reinit(t *testing.T) {
	ctx := &Context{WorkDir: "/original/path"}
	r := NewRegistry(ctx)

	p1 := &mockPluginWithInit{mockPlugin: mockPlugin{id: "p1"}}
	p2 := &mockPluginWithInit{mockPlugin: mockPlugin{id: "p2"}}
	_ = r.Register(p1)
	_ = r.Register(p2)

	// Both plugins should be initialized once
	if p1.initCalls != 1 {
		t.Errorf("p1 init calls = %d, want 1", p1.initCalls)
	}
	if p2.initCalls != 1 {
		t.Errorf("p2 init calls = %d, want 1", p2.initCalls)
	}

	// Start the plugins
	r.Start()
	if !p1.started || !p2.started {
		t.Error("plugins should be started")
	}

	// Now reinitialize with a new path
	newPath := "/new/project/path"
	newProjectRoot := "/new/project/root"
	cmds := r.Reinit(newPath, newProjectRoot)

	// Check that context was updated
	if r.ctx.WorkDir != newPath {
		t.Errorf("context WorkDir = %q, want %q", r.ctx.WorkDir, newPath)
	}
	if r.ctx.ProjectRoot != newProjectRoot {
		t.Errorf("context ProjectRoot = %q, want %q", r.ctx.ProjectRoot, newProjectRoot)
	}

	// Check that plugins were stopped and reinitialized
	if !p1.stopped || !p2.stopped {
		t.Error("plugins should be stopped during Reinit")
	}
	if p1.initCalls != 2 {
		t.Errorf("p1 init calls after Reinit = %d, want 2", p1.initCalls)
	}
	if p2.initCalls != 2 {
		t.Errorf("p2 init calls after Reinit = %d, want 2", p2.initCalls)
	}

	// Check that plugins receive the new context
	if p1.lastCtx == nil || p1.lastCtx.WorkDir != newPath {
		t.Error("p1 should receive context with new WorkDir")
	}
	if p2.lastCtx == nil || p2.lastCtx.WorkDir != newPath {
		t.Error("p2 should receive context with new WorkDir")
	}

	// Should return start commands
	if len(cmds) == 0 {
		t.Error("Reinit should return start commands")
	}
}
