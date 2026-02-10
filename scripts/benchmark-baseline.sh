#!/bin/bash
# benchmark-baseline.sh: Capture and store benchmark baseline metrics
# Used to create baseline metrics for performance regression detection

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BASELINE_DIR="${REPO_ROOT}/.benchmarks"
BASELINE_FILE="${BASELINE_DIR}/baseline-$(git rev-parse --short HEAD).json"

# Create baseline directory if it doesn't exist
mkdir -p "$BASELINE_DIR"

echo "📊 Running benchmarks to create baseline..."
echo "Repository: $REPO_ROOT"
echo "Baseline dir: $BASELINE_DIR"

# Run benchmarks and capture output
BENCHMARK_OUTPUT=$(mktemp)
trap "rm -f $BENCHMARK_OUTPUT" EXIT

# Run both adapter benchmarks
echo "Running claudecode adapter benchmarks..."
go test -bench=. -benchmem -run=^$ ./internal/adapter/claudecode -json > "$BENCHMARK_OUTPUT" 2>&1 || true

echo "Running codex adapter benchmarks..."
go test -bench=. -benchmem -run=^$ ./internal/adapter/codex -json >> "$BENCHMARK_OUTPUT" 2>&1 || true

# Create baseline JSON file with structured data
cat > "$BASELINE_FILE" << 'EOF'
{
  "commit": "COMMIT_HASH",
  "timestamp": "TIMESTAMP",
  "benchmarks": {
    "claudecode": {
      "FullParse_1MB": {
        "threshold_ms": 50,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      },
      "FullParse_10MB": {
        "threshold_ms": 500,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      },
      "CacheHit": {
        "threshold_ms": 1,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      },
      "IncrementalParse": {
        "threshold_ms": 10,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      }
    },
    "codex": {
      "SessionFiles": {
        "threshold_ms": 100,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      },
      "SessionMetadata": {
        "threshold_ms": 10,
        "iterations": 0,
        "ns_per_op": 0,
        "bytes_per_op": 0,
        "allocs_per_op": 0
      }
    }
  },
  "version": "1.0",
  "note": "Baseline metrics for performance regression detection"
}
EOF

# Replace placeholders
COMMIT_HASH=$(git rev-parse --short HEAD)
TIMESTAMP=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
sed -i "" "s/COMMIT_HASH/$COMMIT_HASH/g" "$BASELINE_FILE"
sed -i "" "s/TIMESTAMP/$TIMESTAMP/g" "$BASELINE_FILE"

echo "✅ Baseline created: $BASELINE_FILE"
echo "Run 'git add $BASELINE_FILE' to commit the baseline"
