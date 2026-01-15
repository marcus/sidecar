package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// State holds persistent user preferences.
type State struct {
	GitDiffMode     string `json:"gitDiffMode"`               // "unified" or "side-by-side"
	GitGraphEnabled bool   `json:"gitGraphEnabled,omitempty"` // Show commit graph in sidebar

	// Pane width preferences (percentage of total width, 0 = use default)
	FileBrowserTreeWidth   int `json:"fileBrowserTreeWidth,omitempty"`
	GitStatusSidebarWidth  int `json:"gitStatusSidebarWidth,omitempty"`
	ConversationsSideWidth int `json:"conversationsSideWidth,omitempty"`

	// Plugin-specific state (keyed by working directory path)
	FileBrowser  map[string]FileBrowserState `json:"fileBrowser,omitempty"`
	ActivePlugin map[string]string           `json:"activePlugin,omitempty"`
}

// FileBrowserState holds persistent file browser state.
type FileBrowserState struct {
	SelectedFile   string   `json:"selectedFile,omitempty"`   // Currently selected file path (relative)
	TreeScroll     int      `json:"treeScroll,omitempty"`     // Tree pane scroll offset
	PreviewScroll  int      `json:"previewScroll,omitempty"`  // Preview pane scroll offset
	ExpandedDirs   []string `json:"expandedDirs,omitempty"`   // List of expanded directory paths
	ActivePane     string   `json:"activePane,omitempty"`     // "tree" or "preview"
	PreviewFile    string   `json:"previewFile,omitempty"`    // File being previewed (relative)
	TreeCursor     int      `json:"treeCursor,omitempty"`     // Tree cursor position
}

var (
	current *State
	mu      sync.RWMutex
	path    string
)

// Init loads state from the default location.
func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return InitWithDir(filepath.Join(home, ".config", "sidecar"))
}

// InitWithDir loads state from a specified directory.
// This is primarily for testing to avoid reading real user state.
func InitWithDir(dir string) error {
	path = filepath.Join(dir, "state.json")
	return Load()
}

// Load reads state from disk.
func Load() error {
	mu.Lock()
	defer mu.Unlock()

	current = &State{
		GitDiffMode: "unified", // default
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // no state file yet, use defaults
	}
	if err != nil {
		return err
	}

	return json.Unmarshal(data, current)
}

// Save writes state to disk.
func Save() error {
	mu.RLock()
	defer mu.RUnlock()

	if current == nil {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetGitDiffMode returns the saved diff mode.
func GetGitDiffMode() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return "unified"
	}
	return current.GitDiffMode
}

// SetGitDiffMode saves the diff mode preference.
func SetGitDiffMode(mode string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitDiffMode = mode
	mu.Unlock()
	return Save()
}

// GetGitGraphEnabled returns whether the commit graph is enabled.
func GetGitGraphEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return false
	}
	return current.GitGraphEnabled
}

// SetGitGraphEnabled saves the commit graph preference.
func SetGitGraphEnabled(enabled bool) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitGraphEnabled = enabled
	mu.Unlock()
	return Save()
}

// GetFileBrowserTreeWidth returns the saved file browser tree pane width.
// Returns 0 if no preference is saved (use default).
func GetFileBrowserTreeWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.FileBrowserTreeWidth
}

// SetFileBrowserTreeWidth saves the file browser tree pane width.
func SetFileBrowserTreeWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.FileBrowserTreeWidth = width
	mu.Unlock()
	return Save()
}

// GetGitStatusSidebarWidth returns the saved git status sidebar width.
// Returns 0 if no preference is saved (use default).
func GetGitStatusSidebarWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.GitStatusSidebarWidth
}

// SetGitStatusSidebarWidth saves the git status sidebar width.
func SetGitStatusSidebarWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.GitStatusSidebarWidth = width
	mu.Unlock()
	return Save()
}

// GetConversationsSideWidth returns the saved conversations sidebar width.
// Returns 0 if no preference is saved (use default).
func GetConversationsSideWidth() int {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return 0
	}
	return current.ConversationsSideWidth
}

// SetConversationsSideWidth saves the conversations sidebar width.
func SetConversationsSideWidth(width int) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	current.ConversationsSideWidth = width
	mu.Unlock()
	return Save()
}

// GetFileBrowserState returns the saved file browser state for a given working directory.
func GetFileBrowserState(workdir string) FileBrowserState {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.FileBrowser == nil {
		return FileBrowserState{}
	}
	return current.FileBrowser[workdir]
}

// SetFileBrowserState saves the file browser state for a given working directory.
func SetFileBrowserState(workdir string, fbState FileBrowserState) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.FileBrowser == nil {
		current.FileBrowser = make(map[string]FileBrowserState)
	}
	current.FileBrowser[workdir] = fbState
	mu.Unlock()
	return Save()
}

// GetActivePlugin returns the saved active plugin ID for a given working directory.
func GetActivePlugin(workdir string) string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil || current.ActivePlugin == nil {
		return ""
	}
	return current.ActivePlugin[workdir]
}

// SetActivePlugin saves the active plugin ID for a given working directory.
func SetActivePlugin(workdir, pluginID string) error {
	mu.Lock()
	if current == nil {
		current = &State{}
	}
	if current.ActivePlugin == nil {
		current.ActivePlugin = make(map[string]string)
	}
	current.ActivePlugin[workdir] = pluginID
	mu.Unlock()
	return Save()
}
