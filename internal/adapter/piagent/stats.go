package piagent

// Pi Agent sessions include pre-calculated costs in usage.cost.total,
// so we don't need model-based pricing calculations like Claude Code.
// Cost accumulation happens during metadata parsing in adapter.go.
