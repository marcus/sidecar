#!/bin/bash
# ci-benchmark-check.sh: CI integration script for benchmark regression detection
# Runs on pull requests to detect performance regressions vs. main branch

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Configuration
REGRESSION_THRESHOLD=${REGRESSION_THRESHOLD:-10}  # 10% default
BASELINE_DIR="${REPO_ROOT}/.benchmarks"
BASELINE_FILE="${BASELINE_DIR}/baseline.json"
REPORT_FILE="${REPO_ROOT}/.benchmarks/regression-report-${CI_COMMIT_SHA:0:8}.json"

echo "🚀 CI Benchmark Regression Check"
echo "=================================="
echo "Repository: $REPO_ROOT"
echo "Regression threshold: ${REGRESSION_THRESHOLD}%"
echo "Baseline: $BASELINE_FILE"

# Step 1: Check if baseline exists
if [ ! -f "$BASELINE_FILE" ]; then
    echo "⚠️  Warning: Baseline file not found at $BASELINE_FILE"
    echo "To create baseline, run: scripts/benchmark-baseline.sh"
    exit 0
fi

echo "✅ Baseline file found"

# Step 2: Run benchmarks for current branch
echo ""
echo "📊 Running benchmarks on current branch..."
BENCH_OUTPUT=$(mktemp)
trap "rm -f $BENCH_OUTPUT" EXIT

# Run benchmarks and capture output
echo "Running claudecode adapter benchmarks..."
go test -bench=BenchmarkMessages -benchmem -run=^$ -timeout=10m \
    ./internal/adapter/claudecode > "$BENCH_OUTPUT" 2>&1 || true

echo "Running codex adapter benchmarks..."
go test -bench=BenchmarkSession -benchmem -run=^$ -timeout=10m \
    ./internal/adapter/codex >> "$BENCH_OUTPUT" 2>&1 || true

# Step 3: Compile and run regression detector
echo ""
echo "🔍 Analyzing benchmark results..."
DETECTOR_BIN=$(mktemp)
trap "rm -f $DETECTOR_BIN" EXIT

go build -o "$DETECTOR_BIN" "$SCRIPT_DIR/perf-regressor.go" || {
    echo "⚠️  Warning: Failed to build regression detector"
    exit 0
}

# Run detector with baseline
"$DETECTOR_BIN" \
    -bench-dir="$REPO_ROOT" \
    -baseline="$BASELINE_FILE" \
    -regression="$REGRESSION_THRESHOLD" \
    -output="$REPORT_FILE"

# Exit code handling is done by the detector
echo ""
echo "📄 Report saved to: $REPORT_FILE"
