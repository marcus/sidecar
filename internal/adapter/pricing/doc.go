// Package pricing calculates token costs for Claude API models.
//
// It maps model ID strings (e.g. "claude-opus-4-6-20260101") to version-aware
// pricing tiers and computes dollar costs from token usage. Cache read and
// cache write tokens are priced as fractions of the base input rate.
package pricing
