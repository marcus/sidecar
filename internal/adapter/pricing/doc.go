// Package pricing calculates API costs for Claude model usage based on
// token counts. It classifies model IDs into pricing tiers by family
// (Opus, Sonnet, Haiku) and version, then applies per-million-token
// rates for input, output, cache-read, and cache-write tokens.
package pricing
