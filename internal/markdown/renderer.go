package markdown

import (
	"log"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/charmbracelet/glamour"
	"github.com/marcus/sidecar/internal/styles"
)

const (
	// MinWidthForMarkdown is the minimum terminal width for markdown rendering.
	// Below this, falls back to plain text wrapping.
	MinWidthForMarkdown = 30

	// MaxCacheEntries is the maximum number of cached renders before eviction.
	MaxCacheEntries = 100
)

// Renderer wraps Glamour for markdown rendering with caching.
type Renderer struct {
	mu        sync.RWMutex
	renderer  *glamour.TermRenderer
	lastWidth int
	cache     map[uint64][]string
}

// NewRenderer creates a new markdown renderer instance.
func NewRenderer() (*Renderer, error) {
	return &Renderer{
		cache: make(map[uint64][]string),
	}, nil
}

// RenderContent renders markdown content to styled lines.
func (r *Renderer) RenderContent(content string, width int) []string {
	if width < MinWidthForMarkdown {
		return WrapText(content, width)
	}

	if content == "" {
		return []string{}
	}

	key := r.cacheKey(content, width)

	// Check cache first (read lock)
	r.mu.RLock()
	if cached, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Need to render (write lock)
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check cache after acquiring write lock
	if cached, ok := r.cache[key]; ok {
		return cached
	}

	// Get or create renderer for this width
	renderer, err := r.getOrCreateRenderer(width)
	if err != nil {
		log.Printf("glamour renderer error: %v", err)
		return WrapText(content, width)
	}

	// Render markdown
	rendered, err := renderer.Render(content)
	if err != nil {
		log.Printf("glamour render error: %v", err)
		return WrapText(content, width)
	}

	// Trim trailing whitespace and split into lines
	rendered = strings.TrimRight(rendered, "\n\r\t ")
	lines := strings.Split(rendered, "\n")

	// Cache eviction if needed
	if len(r.cache) >= MaxCacheEntries {
		r.cache = make(map[uint64][]string)
	}
	r.cache[key] = lines

	return lines
}

// cacheKey generates a cache key from content and width using xxhash.
func (r *Renderer) cacheKey(content string, width int) uint64 {
	h := xxhash.New()
	h.WriteString(content)
	h.Write([]byte{byte(width >> 8), byte(width)})
	return h.Sum64()
}

// getOrCreateRenderer lazily creates or recreates the renderer for the given width.
// Must be called with write lock held.
func (r *Renderer) getOrCreateRenderer(width int) (*glamour.TermRenderer, error) {
	if r.renderer != nil && r.lastWidth == width {
		return r.renderer, nil
	}

	// Width changed or first use - create new renderer and clear cache
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(styles.GetMarkdownTheme()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}

	r.renderer = renderer
	r.lastWidth = width
	r.cache = make(map[uint64][]string) // Clear cache on width change

	return renderer, nil
}

// WrapText wraps text to fit within maxWidth.
// Used as fallback when terminal is too narrow for markdown rendering.
func WrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	// Replace newlines with spaces for simpler wrapping
	text = strings.ReplaceAll(text, "\n", " ")

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return lines
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= maxWidth {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}
