# Performance Regression Detection

This document describes the performance regression detection system for Sidecar, which monitors benchmark metrics across commits to catch performance degradations early.

## Overview

The system consists of:

1. **Baseline Metrics** (`.benchmarks/baseline.json`) - Reference performance metrics from the main branch
2. **Benchmark Suite** - Go benchmarks in `internal/adapter/claudecode` and `internal/adapter/codex`
3. **Regression Detector** (`scripts/perf-regressor.go`) - Compares current benchmarks against baseline
4. **CI Integration** (`scripts/ci-benchmark-check.sh`) - Runs regression checks on pull requests
5. **GitHub Actions Workflow** (`.github/workflows/benchmark.yml`) - Orchestrates benchmark runs in CI

## Performance Targets

### ClaudeCode Adapter

| Benchmark | Threshold | Description |
|-----------|-----------|-------------|
| `FullParse_1MB` | < 50ms | Parse 1MB session file (full parse, no cache) |
| `FullParse_10MB` | < 500ms | Parse 10MB session file (full parse, no cache) |
| `CacheHit` | < 1ms | Retrieve cached session (cache hit) |
| `IncrementalParse` | < 10ms | Incremental parse for newly appended messages |

These targets correspond to benchmarks in `internal/adapter/claudecode/adapter_bench_test.go`.

### Codex Adapter

| Benchmark | Threshold | Description |
|-----------|-----------|-------------|
| `SessionFiles` | < 100ms | Directory walk for 100+ session files |
| `SessionMetadata` | < 10ms | Parse session metadata from file |
| `SessionMetadataCached` | < 1ms | Metadata cache hit |
| `Sessions` | < 50ms | Full Sessions() call across multiple files |

These targets correspond to benchmarks in `internal/adapter/codex/adapter_bench_test.go`.

## Creating a Baseline

To establish a baseline from the current main branch:

```bash
scripts/benchmark-baseline.sh
```

This generates `.benchmarks/baseline-<commit>.json`. The baseline should be created from a stable version of main and committed to the repository.

## Running Regression Detection Locally

```bash
# Run benchmarks and detect regressions against baseline
scripts/ci-benchmark-check.sh

# Or manually run the detector
go run scripts/perf-regressor.go \
  -baseline=.benchmarks/baseline.json \
  -regression=10 \
  -output=.benchmarks/report.json
```

## Regression Threshold

By default, a regression is flagged if performance degrades by **more than 10%** relative to the baseline.

- Benchmark threshold: 50ms (target)
- Current performance: 56ms (measured)
- Degradation: (56 - 50) / 50 * 100 = **12%** ❌ REGRESSION

To adjust the threshold (e.g., allow 15% degradation):

```bash
scripts/ci-benchmark-check.sh
# Set: REGRESSION_THRESHOLD=15
```

## CI Integration

On pull requests, GitHub Actions runs the benchmark regression check:

1. Fetch baseline metrics from main
2. Run full benchmark suite on PR branch
3. Compare against baseline
4. Report regressions with detailed breakdown
5. Fail PR checks if critical regressions detected

## Interpreting Results

A regression report (JSON) includes:

```json
{
  "timestamp": "2025-02-09T12:34:56Z",
  "total_benchmarks": 8,
  "regression_count": 1,
  "passed_threshold": false,
  "regressions": [
    {
      "benchmark": "claudecode.FullParse_1MB",
      "threshold_ms": 50,
      "actual_ms": 58,
      "degradation_percent": 16.0,
      "status": "FAIL"
    }
  ],
  "summary": "❌ 1/8 benchmarks exceeded performance thresholds (regression >10%)"
}
```

## When Regressions Occur

If a regression is detected:

1. **Profile the change** - Use `go tool pprof` to identify hotspots
2. **Optimize** - Address the bottleneck
3. **Verify** - Re-run benchmarks locally
4. **Update baseline** (if intentional) - If the change is justified, update the baseline metrics

Example profiling:

```bash
# Profile a specific benchmark
go test -bench=BenchmarkMessages_FullParse_1MB -benchmem -cpuprofile=cpu.prof ./internal/adapter/claudecode
go tool pprof -http=:8080 cpu.prof
```

## Acceptable Regressions

In some cases, small regressions are acceptable:

- **Correctness improvements** - Fixing bugs may slow down code slightly
- **Feature additions** - New functionality may add small overhead
- **Structural changes** - Refactoring for maintainability can impact performance

In these cases:

1. Document the reason for the regression in the PR
2. Include benchmark results in the PR description
3. Discuss trade-offs with reviewers
4. Update baseline only with explicit approval

## Managing Baselines

Baselines should be:

- ✅ Created from stable main branch
- ✅ Committed to the repository
- ✅ Updated when intentional performance changes are made
- ❌ Updated without review (can mask performance regressions)

To update the baseline:

```bash
# Update baseline from current branch
scripts/benchmark-baseline.sh

# Commit the new baseline
git add .benchmarks/baseline.json
git commit -m "perf: update performance baseline"
```

## Continuous Monitoring

Over time, track performance trends:

```bash
# View recent baseline changes
git log --oneline .benchmarks/baseline*.json

# Compare two baselines
git diff HEAD~1 .benchmarks/baseline.json | jq .
```

## Future Enhancements

Potential improvements:

- **Historical tracking** - Store time-series performance metrics
- **Comparative analysis** - Compare against multiple baselines (e.g., last 10 commits)
- **Per-package reporting** - Break down regressions by adapter/component
- **Automated optimization** - Flag regressions for automatic profiling in CI
- **Performance budgets** - Set per-function performance budgets

## References

- [Go Benchmark Documentation](https://golang.org/pkg/testing/#B)
- [Go pprof Documentation](https://golang.org/pkg/runtime/pprof/)
- [Sidecar Benchmarks](./internal/adapter/claudecode/adapter_bench_test.go)
