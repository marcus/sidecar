package image

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blacktop/go-termimg"
)

const (
	MaxImageSize    = 10 * 1024 * 1024 // 10MB
	MaxCacheEntries = 20
)

// Supported image extensions
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".ico":  true,
}

// Protocol represents terminal graphics capability
type Protocol int

const (
	ProtocolNone Protocol = iota
	ProtocolKitty
	ProtocolITerm2
	ProtocolSixel
)

// String returns human-readable protocol name
func (p Protocol) String() string {
	switch p {
	case ProtocolKitty:
		return "Kitty"
	case ProtocolITerm2:
		return "iTerm2"
	case ProtocolSixel:
		return "Sixel"
	default:
		return "None"
	}
}

// IsImageFile checks if path has a supported image extension
func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

// RenderResult contains rendered output or fallback
type RenderResult struct {
	Content    string // ANSI escape sequences for image
	IsFallback bool   // True if showing text message
	Width      int
	Height     int
}

// cacheKey uniquely identifies a rendered image
type cacheKey struct {
	Path   string
	Width  int
	Height int
	Mtime  int64
}

// Renderer handles image preview rendering
type Renderer struct {
	protocol Protocol
	cache    map[cacheKey]*RenderResult
	order    []cacheKey // LRU order
	mu       sync.RWMutex
}

// New creates a renderer and detects protocol
func New() *Renderer {
	return &Renderer{
		protocol: detectProtocol(),
		cache:    make(map[cacheKey]*RenderResult),
	}
}

// Protocol returns detected protocol
func (r *Renderer) Protocol() Protocol {
	return r.protocol
}

// detectProtocol queries terminal for graphics support
func detectProtocol() Protocol {
	proto := termimg.DetectProtocol()
	switch proto {
	case termimg.Kitty:
		return ProtocolKitty
	case termimg.ITerm2:
		return ProtocolITerm2
	case termimg.Sixel:
		return ProtocolSixel
	default:
		return ProtocolNone
	}
}

// Render renders an image to terminal graphics using Halfblocks protocol.
// Halfblocks uses Unicode block characters (▀▄█) with ANSI colors, which
// integrates properly with TUI frameworks like Bubble Tea. Native protocols
// (Kitty, iTerm2, Sixel) bypass TUI rendering and write directly to terminal.
func (r *Renderer) Render(path string, maxW, maxH int) (*RenderResult, error) {
	// Stat file for size and mtime
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Check size limit
	if stat.Size() > MaxImageSize {
		return &RenderResult{
			IsFallback: true,
			Content:    "Image too large (>10MB)",
		}, nil
	}

	// Check cache
	key := cacheKey{Path: path, Width: maxW, Height: maxH, Mtime: stat.ModTime().UnixNano()}
	r.mu.RLock()
	if cached, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	// Load and render image via go-termimg
	img, err := termimg.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}

	// Use Halfblocks protocol for TUI compatibility
	// Halfblocks renders using Unicode block characters that work within
	// text-based TUI frameworks, unlike Kitty/iTerm2/Sixel which bypass
	// the TUI and write directly to terminal
	rendered, err := img.
		Width(maxW).
		Height(maxH).
		Scale(termimg.ScaleFit).
		Protocol(termimg.Halfblocks).
		Render()
	if err != nil {
		return nil, fmt.Errorf("render image: %w", err)
	}

	result := &RenderResult{
		Content: rendered,
		Width:   maxW,
		Height:  maxH,
	}

	// Cache result
	r.mu.Lock()
	r.cache[key] = result
	r.order = append(r.order, key)
	// LRU eviction
	for len(r.cache) > MaxCacheEntries {
		delete(r.cache, r.order[0])
		r.order = r.order[1:]
	}
	r.mu.Unlock()

	return result, nil
}

// ClearCache clears the render cache
func (r *Renderer) ClearCache() {
	r.mu.Lock()
	r.cache = make(map[cacheKey]*RenderResult)
	r.order = nil
	r.mu.Unlock()
}

// InvalidatePath removes cached entries for a specific path
func (r *Renderer) InvalidatePath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newOrder := make([]cacheKey, 0, len(r.order))
	for _, key := range r.order {
		if key.Path == path {
			delete(r.cache, key)
		} else {
			newOrder = append(newOrder, key)
		}
	}
	r.order = newOrder
}

// CacheStats returns cache statistics for debugging
func (r *Renderer) CacheStats() (entries int, maxEntries int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache), MaxCacheEntries
}

// SupportedTerminals returns a string about image preview support
func SupportedTerminals() string {
	return "all terminals with Unicode and true color support"
}
