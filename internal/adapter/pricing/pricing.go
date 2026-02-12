package pricing

import (
	"strconv"
	"strings"
)

// Usage holds token counts for cost calculation.
type Usage struct {
	InputTokens  int // Non-cache input tokens (API's input_tokens field)
	OutputTokens int
	CacheRead    int // cache_read_input_tokens
	CacheWrite   int // cache_creation_input_tokens
}

// modelTier identifies a pricing tier.
type modelTier struct {
	inRate  float64 // dollars per million input tokens
	outRate float64 // dollars per million output tokens
}

var (
	// Version-aware tiers.
	tierOpusNew  = modelTier{5.0, 25.0}  // Opus 4.5+
	tierOpusOld  = modelTier{15.0, 75.0} // Opus 3/4/4.1
	tierSonnet   = modelTier{3.0, 15.0}  // All Sonnet versions
	tierHaikuNew = modelTier{1.0, 5.0}   // Haiku 4.5+
	tierHaiku35  = modelTier{0.80, 4.0}  // Haiku 3.5
	tierHaikuOld = modelTier{0.25, 1.25} // Haiku 3
	tierDefault  = tierSonnet            // Unknown models
)

// ModelCost calculates cost in dollars for the given model and usage.
func ModelCost(model string, usage Usage) float64 {
	tier := classifyModel(model)

	inputCost := float64(usage.InputTokens) * tier.inRate / 1_000_000
	cacheReadCost := float64(usage.CacheRead) * tier.inRate * 0.1 / 1_000_000
	cacheWriteCost := float64(usage.CacheWrite) * tier.inRate * 1.25 / 1_000_000
	outputCost := float64(usage.OutputTokens) * tier.outRate / 1_000_000

	return inputCost + cacheReadCost + cacheWriteCost + outputCost
}

// classifyModel determines the pricing tier for a model ID string.
func classifyModel(model string) modelTier {
	lower := strings.ToLower(model)

	switch {
	case strings.Contains(lower, "opus"):
		major, minor := extractVersion(lower, "opus")
		if major > 4 || (major == 4 && minor >= 5) {
			return tierOpusNew
		}
		return tierOpusOld

	case strings.Contains(lower, "sonnet"):
		return tierSonnet

	case strings.Contains(lower, "haiku"):
		major, minor := extractVersion(lower, "haiku")
		if major > 4 || (major == 4 && minor >= 5) {
			return tierHaikuNew
		}
		if major == 3 && minor == 5 {
			return tierHaiku35
		}
		return tierHaikuOld

	default:
		return tierDefault
	}
}

// extractVersion extracts major.minor version associated with a model family name.
// Handles both formats:
//   - New: "claude-opus-4-6-20260101"  -> family="opus", version segments after family
//   - Old: "claude-3-5-sonnet-20241022" -> family="sonnet", version segments before family
func extractVersion(model, family string) (major, minor int) {
	idx := strings.Index(model, family)
	if idx < 0 {
		return 0, 0
	}

	// Try after the family name: "opus-4-6-..." or "opus-4-5-..."
	after := model[idx+len(family):]
	if m, n, ok := parseVersionDash(after); ok {
		return m, n
	}

	// Try before the family name: "...-3-5-sonnet" or "...-3-opus"
	before := model[:idx]
	if m, n, ok := parseVersionDashReverse(before); ok {
		return m, n
	}

	return 0, 0
}

// parseVersionDash tries to parse "-MAJOR-MINOR" or "-MAJOR" from the start of s.
// Rejects numbers >= 100 which are likely date stamps (e.g., 20250101).
func parseVersionDash(s string) (major, minor int, ok bool) {
	if len(s) == 0 || s[0] != '-' {
		return 0, 0, false
	}
	s = s[1:] // skip leading dash

	parts := strings.SplitN(s, "-", 3)
	if len(parts) == 0 {
		return 0, 0, false
	}

	m, err := strconv.Atoi(parts[0])
	if err != nil || m >= 100 {
		return 0, 0, false
	}

	n := 0
	if len(parts) >= 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v < 100 {
			n = v
		}
	}

	return m, n, true
}

// parseVersionDashReverse tries to parse "MAJOR-MINOR-" or "MAJOR-" from the end of s.
// s is the part before the family name, e.g., "claude-3-5-" or "claude-3-".
func parseVersionDashReverse(s string) (major, minor int, ok bool) {
	// Strip trailing dash
	s = strings.TrimRight(s, "-")
	if s == "" {
		return 0, 0, false
	}

	// Find version segments from the end
	parts := strings.Split(s, "-")
	if len(parts) == 0 {
		return 0, 0, false
	}

	// Last part should be major or minor
	last := parts[len(parts)-1]
	v1, err := strconv.Atoi(last)
	if err != nil {
		return 0, 0, false
	}

	// Check if second-to-last is also a number (major-minor)
	if len(parts) >= 2 {
		secondLast := parts[len(parts)-2]
		if v0, err := strconv.Atoi(secondLast); err == nil {
			return v0, v1, true
		}
	}

	// Single number: treat as major version with minor=0
	return v1, 0, true
}
