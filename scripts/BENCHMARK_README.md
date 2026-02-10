# Performance Regression Detection Scripts

This directory contains scripts for Sidecar's performance regression detection system.

## Files

### `benchmark-baseline.sh`
Creates a baseline benchmark metrics file from the current code state.

```bash
./scripts/benchmark-baseline.sh
```

Creates `.benchmarks/baseline-<commit>.json` with performance thresholds for:
- ClaudeCode adapter (file parsing)
- Codex adapter (session handling)

**When to use**: Create a baseline after significant refactoring or on main branch updates.

### `ci-benchmark-check.sh`
CI integration script that runs benchmarks and detects regressions.

```bash
REGRESSION_THRESHOLD=10 ./scripts/ci-benchmark-check.sh
```

**Environment variables**:
- `REGRESSION_THRESHOLD` - Percentage degradation threshold (default: 10%)

**When to use**: Automatically run in CI/CD pipelines on pull requests.

### `perf-regressor.go`
Standalone tool that compares benchmark results against baseline and generates reports.

```bash
go run scripts/perf-regressor.go \
  -baseline=.benchmarks/baseline.json \
  -regression=10 \
  -output=report.json
```

**Flags**:
- `-baseline` - Path to baseline metrics (default: `.benchmarks/baseline.json`)
- `-bench-dir` - Directory to run benchmarks in (default: current directory)
- `-regression` - Regression threshold in percentage (default: 10)
- `-output` - Output file for JSON report (default: stdout)
- `-fail` - Exit with error on regression (default: true)

## Workflow

### Local Development

```bash
# 1. Make changes
# 2. Run regression check
scripts/ci-benchmark-check.sh

# 3. If regression detected, profile and optimize
go test -bench=BenchmarkMessages_FullParse_1MB -cpuprofile=cpu.prof ./internal/adapter/claudecode
go tool pprof -http=:8080 cpu.prof
```

### CI/CD Integration

The GitHub Actions workflow (`.github/workflows/benchmark.yml`) automatically:

1. **On PR**: Compares against main branch baseline
2. **On push to main**: Creates/updates baseline
3. **Comments on PR** with regression report

### Creating/Updating Baseline

```bash
# Create baseline from current state
scripts/benchmark-baseline.sh

# Review changes
git diff .benchmarks/baseline*.json

# Commit (if intentional performance changes)
git add .benchmarks/baseline-*.json
git commit -m "perf: update baseline after optimization"
```

## Interpreting Results

### Passing Benchmarks
```json
{
  "summary": "✅ All 8 benchmarks passed performance thresholds",
  "passed_threshold": true,
  "regression_count": 0
}
```

### Regressions Detected
```json
{
  "summary": "❌ 1/8 benchmarks exceeded thresholds (regression >10%)",
  "regressions": [{
    "benchmark": "claudecode.FullParse_1MB",
    "threshold_ms": 50,
    "actual_ms": 58,
    "degradation_percent": 16.0,
    "status": "FAIL"
  }]
}
```

## Performance Targets

| Component | Benchmark | Threshold |
|-----------|-----------|-----------|
| ClaudeCode | FullParse_1MB | < 50ms |
| ClaudeCode | FullParse_10MB | < 500ms |
| ClaudeCode | CacheHit | < 1ms |
| ClaudeCode | IncrementalParse | < 10ms |
| Codex | SessionFiles | < 100ms |
| Codex | SessionMetadata | < 10ms |

See `REGRESSION.md` for detailed documentation.

## Profiling Guide

When a regression is detected:

```bash
# Profile the benchmark
go test -bench=BenchmarkMessages_FullParse_1MB \
  -benchmem \
  -cpuprofile=cpu.prof \
  -memprofile=mem.prof \
  ./internal/adapter/claudecode

# View CPU profile
go tool pprof cpu.prof
(pprof) top
(pprof) list main.functionName

# View memory profile
go tool pprof mem.prof
```

## Development

### Adding New Benchmarks

1. Add benchmark functions to adapter test files:
   ```go
   func BenchmarkNewOperation(b *testing.B) {
       // Setup
       b.ReportAllocs()
       b.ResetTimer()
       for i := 0; i < b.N; i++ {
           // Operation
       }
   }
   ```

2. Update baseline with new benchmark names
3. Set appropriate threshold in `.benchmarks/baseline.json`

### Modifying Detection Logic

Edit `perf-regressor.go`:
- `detectRegressions()` - Core regression detection
- `parseBenchmarkOutput()` - Benchmark output parsing
- `runBenchmarks()` - Benchmark execution
- `outputReport()` - Report formatting

## Troubleshooting

### "Baseline file not found"
Create baseline: `scripts/benchmark-baseline.sh`

### Benchmarks too slow/fast
- Verify machine specs haven't changed
- Check for background processes affecting results
- Update baseline if changes are intentional

### CI always shows regression
- Check git history for performance changes
- Baseline may be stale - update from main
- Increase regression threshold if needed

## See Also

- `REGRESSION.md` - Detailed performance regression documentation
- `.github/workflows/benchmark.yml` - CI/CD workflow
- `.benchmarks/baseline.json` - Current performance baseline
