package ui

import "github.com/mattn/go-runewidth"

// TruncateString truncates a string to the given visual width.
// It handles multi-byte characters and full-width characters correctly.
// If the string is truncated, "..." is appended (and accounted for in width).
// Pre-condition: width should be at least 3.
func TruncateString(s string, width int) string {
	if width < 3 {
		// Fallback for very small width
		runes := []rune(s)
		if len(runes) > width {
			return string(runes[:width])
		}
		return s
	}

	if runewidth.StringWidth(s) <= width {
		return s
	}

	targetWidth := width - 3

	currentWidth := 0
	runes := []rune(s)
	for i, r := range runes {
		w := runewidth.RuneWidth(r)
		if currentWidth+w > targetWidth {
			return string(runes[:i]) + "..."
		}
		currentWidth += w
	}

	return s
}

// SafeByteSlice extracts a substring using byte positions, ensuring
// the slice boundaries fall on valid UTF-8 boundaries.
// Returns the substring or empty string if positions are invalid.
func SafeByteSlice(s string, byteStart, byteEnd int) string {
	if byteStart < 0 {
		byteStart = 0
	}
	if byteEnd > len(s) {
		byteEnd = len(s)
	}
	if byteStart >= byteEnd || byteStart >= len(s) {
		return ""
	}

	// Convert to runes and find boundaries
	runes := []rune(s)
	bytePos := 0
	runeStart := 0
	runeEnd := len(runes)

	for i, r := range runes {
		if bytePos <= byteStart && bytePos+len(string(r)) > byteStart {
			runeStart = i
		}
		if bytePos < byteEnd {
			runeEnd = i + 1
		}
		bytePos += len(string(r))
		if bytePos >= byteEnd {
			break
		}
	}

	if runeStart >= runeEnd {
		return ""
	}
	return string(runes[runeStart:runeEnd])
}

// TruncateMid truncates a string to fit width, centering around a visual position.
// Returns (truncated string, new highlight start rune index, new highlight end rune index).
// If the original highlight fits, indices are adjusted for any leading truncation.
func TruncateMid(s string, width int, highlightRuneStart, highlightRuneEnd int) (string, int, int) {
	runes := []rune(s)
	totalWidth := runewidth.StringWidth(s)

	if totalWidth <= width {
		return s, highlightRuneStart, highlightRuneEnd
	}

	if width < 6 {
		width = 6 // Min for "...x..."
	}

	// Calculate the center of highlight
	highlightCenter := (highlightRuneStart + highlightRuneEnd) / 2
	usableWidth := width - 6 // Reserve for "..." on both ends

	// Calculate start rune position
	startRune := highlightCenter - usableWidth/2
	if startRune < 0 {
		startRune = 0
	}

	// Build result tracking visual width
	var result []rune
	currentWidth := 0
	needPrefix := startRune > 0
	needSuffix := false

	// Add prefix ellipsis
	if needPrefix {
		result = append(result, '.', '.', '.')
		currentWidth = 3
	}

	// Add content
	newHighlightStart := -1
	newHighlightEnd := -1

	for i := startRune; i < len(runes); i++ {
		r := runes[i]
		w := runewidth.RuneWidth(r)

		if currentWidth+w > width-3 { // Reserve for suffix
			needSuffix = true
			break
		}

		if i == highlightRuneStart {
			newHighlightStart = len(result)
		}
		result = append(result, r)
		currentWidth += w
		if i == highlightRuneEnd-1 {
			newHighlightEnd = len(result)
		}
	}

	if needSuffix {
		result = append(result, '.', '.', '.')
	}

	// Adjust highlight indices if they weren't set
	if newHighlightStart == -1 {
		newHighlightStart = 0
	}
	if newHighlightEnd == -1 {
		newHighlightEnd = len(result)
	}

	return string(result), newHighlightStart, newHighlightEnd
}

// BytePosToRunePos converts a byte position to a rune position.
func BytePosToRunePos(s string, bytePos int) int {
	if bytePos <= 0 {
		return 0
	}
	if bytePos >= len(s) {
		return len([]rune(s))
	}

	pos := 0
	for i := range s {
		if i >= bytePos {
			return pos
		}
		pos++
	}
	return pos
}

// TruncateStart truncates the start of the string if it exceeds width.
// "..." + suffix
func TruncateStart(s string, width int) string {
	if width < 3 {
		runes := []rune(s)
		if len(runes) > width {
			return string(runes[len(runes)-width:])
		}
		return s
	}

	if runewidth.StringWidth(s) <= width {
		return s
	}

	targetWidth := width - 3
	runes := []rune(s)

	// Calculate total width first
	totalWidth := 0
	for _, r := range runes {
		totalWidth += runewidth.RuneWidth(r)
	}

	// Scan from end
	currentWidth := 0
	for i := len(runes) - 1; i >= 0; i-- {
		w := runewidth.RuneWidth(runes[i])
		if currentWidth+w > targetWidth {
			return "..." + string(runes[i+1:])
		}
		currentWidth += w
	}

	return s
}
