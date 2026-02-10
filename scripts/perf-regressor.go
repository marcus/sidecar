package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BenchmarkResult represents a single benchmark result
type BenchmarkResult struct {
	Name        string
	Iterations  int64
	NsPerOp     int64
	BytesPerOp  int64
	AllocsPerOp int64
}

// BenchmarkBaseline contains baseline metrics and thresholds
type BenchmarkBaseline struct {
	Commit     string                                   `json:"commit"`
	Timestamp  string                                   `json:"timestamp"`
	Version    string                                   `json:"version"`
	Benchmarks map[string]map[string]MetricThreshold   `json:"benchmarks"`
}

// MetricThreshold contains performance threshold for a benchmark
type MetricThreshold struct {
	ThresholdMs int64 `json:"threshold_ms"`
	Iterations  int64 `json:"iterations"`
	NsPerOp     int64 `json:"ns_per_op"`
	BytesPerOp  int64 `json:"bytes_per_op"`
	AllocsPerOp int64 `json:"allocs_per_op"`
}

// RegressionReport contains the regression analysis results
type RegressionReport struct {
	Timestamp       string            `json:"timestamp"`
	TotalBench      int               `json:"total_benchmarks"`
	RegressionCount int               `json:"regression_count"`
	Regressions     []RegressionDetail `json:"regressions"`
	Warnings        []string          `json:"warnings"`
	Summary         string            `json:"summary"`
	PassedThreshold bool              `json:"passed_threshold"`
}

// RegressionDetail contains details of a single regression
type RegressionDetail struct {
	Benchmark      string  `json:"benchmark"`
	ThresholdMs    int64   `json:"threshold_ms"`
	ActualMs       int64   `json:"actual_ms"`
	DegradationPct float64 `json:"degradation_percent"`
	Status         string  `json:"status"`
}

// BenchmarkLine represents a parsed benchmark line from 'go test' output
type BenchmarkLine struct {
	Name        string
	Package     string
	Iterations  int64
	NsPerOp     int64
	BytesPerOp  int64
	AllocsPerOp int64
}

// BenchmarkNameMapping maps full benchmark names to baseline categories
// Key: "PackageName_BenchmarkName" or subtest -> Value: (category, baseline name)
var BenchmarkNameMapping = map[string][2]string{
	// ClaudeCode benchmarks
	"BenchmarkMessages_FullParse_1MB":  {"claudecode", "FullParse_1MB"},
	"BenchmarkMessages_FullParse_10MB": {"claudecode", "FullParse_10MB"},
	"BenchmarkMessages_CacheHit":       {"claudecode", "CacheHit"},
	"BenchmarkMessages_IncrementalParse": {"claudecode", "IncrementalParse"},
	"BenchmarkMessages_Allocs":         {"claudecode", "Allocs"},
	"BenchmarkSessions_50Files":        {"claudecode", "Sessions_50Files"},

	// Codex benchmarks
	"BenchmarkSessionFiles":       {"codex", "SessionFiles"},
	"BenchmarkSessionFilesCached": {"codex", "SessionFilesCached"},
	"BenchmarkSessionMetadataSmall": {"codex", "SessionMetadataSmall"},
	"BenchmarkSessionMetadataLarge": {"codex", "SessionMetadataLarge"},
	"BenchmarkSessionMetadataCached": {"codex", "SessionMetadataCached"},
	"BenchmarkCwdMatchesProject":  {"codex", "CwdMatchesProject"},
	"BenchmarkResolvedProjectPath": {"codex", "ResolvedProjectPath"},
}

var (
	baselineFile    = flag.String("baseline", ".benchmarks/baseline.json", "path to baseline metrics file")
	benchOutputFile = flag.String("bench-output", "", "path to benchmark output file (if empty, runs benchmarks)")
	regressionPct   = flag.Float64("regression", 10.0, "regression threshold in percentage (default 10%)")
	outputFile      = flag.String("output", "", "output file for regression report (default: stdout)")
	failOnRegression = flag.Bool("fail", true, "exit with error code if regression detected")
)

func main() {
	flag.Parse()

	// Load baseline
	baseline, err := loadBaseline(*baselineFile)
	if err != nil {
		log.Fatalf("Failed to load baseline: %v", err)
	}

	// Get benchmark output
	var output string
	if *benchOutputFile != "" {
		// Read from file
		data, err := os.ReadFile(*benchOutputFile)
		if err != nil {
			log.Fatalf("Failed to read benchmark output file: %v", err)
		}
		output = string(data)
	} else {
		// Run benchmarks in-process
		var err error
		output, err = runBenchmarks()
		if err != nil {
			log.Fatalf("Failed to run benchmarks: %v", err)
		}
	}

	// Parse benchmark results
	results := parseBenchmarkOutput(output)

	// Detect regressions
	report := detectRegressions(baseline, results, *regressionPct)

	// Output report
	outputReport(report, *outputFile)

	// Exit with appropriate code
	if *failOnRegression && !report.PassedThreshold {
		os.Exit(1)
	}
}

func loadBaseline(path string) (*BenchmarkBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}

	var baseline BenchmarkBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}

	return &baseline, nil
}

func runBenchmarks() (string, error) {
	packages := []string{
		"./internal/adapter/claudecode",
		"./internal/adapter/codex",
	}

	var allOutput strings.Builder

	for _, pkg := range packages {
		cmd := exec.Command("go", "test", "-bench=.", "-benchmem", "-run=^$", "-timeout=10m", pkg)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Continue on error - some benchmarks may be skipped
			log.Printf("Warning: benchmark command failed for %s: %v", pkg, err)
		}
		allOutput.Write(output)
	}

	return allOutput.String(), nil
}

func parseBenchmarkOutput(output string) map[string]*BenchmarkResult {
	results := make(map[string]*BenchmarkResult)

	scanner := bufio.NewScanner(strings.NewReader(output))
	// Pattern: Benchmark<Name>-N    <iterations> <ns/op> <B/op> <allocs/op>
	// Example: BenchmarkMessages_FullParse_1MB-8    10    100000000 ns/op    256 B/op    5 allocs/op
	// Also handles subtests: BenchmarkSessions/n=50-8    10    100000000 ns/op    256 B/op    5 allocs/op
	// Handles floating-point ns/op: BenchmarkSessionFilesCached-14    1000000    34.78 ns/op
	pattern := regexp.MustCompile(`^(Benchmark\S+?(?:/\S+)?)-\d+\s+(\d+)\s+([0-9.]+)\s+ns/op(?:\s+(\d+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := pattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		fullName := matches[1]
		iter, _ := strconv.ParseInt(matches[2], 10, 64)

		// Parse ns/op as float and convert to int64
		nsOpFloat, _ := strconv.ParseFloat(matches[3], 64)
		nsOp := int64(nsOpFloat)

		bytesOp := int64(0)
		allocsOp := int64(0)

		if matches[4] != "" {
			bytesOp, _ = strconv.ParseInt(matches[4], 10, 64)
		}
		if matches[5] != "" {
			allocsOp, _ = strconv.ParseInt(matches[5], 10, 64)
		}

		// Handle subtests like "BenchmarkSessions/n=50" -> map to "Sessions_50"
		benchName := fullName
		if strings.Contains(benchName, "/") {
			parts := strings.Split(benchName, "/")
			baseName := parts[0]
			subtest := strings.TrimPrefix(parts[1], "n=")
			benchName = baseName + "_" + subtest
		}

		results[benchName] = &BenchmarkResult{
			Name:        benchName,
			Iterations:  iter,
			NsPerOp:     nsOp,
			BytesPerOp:  bytesOp,
			AllocsPerOp: allocsOp,
		}
	}

	return results
}

func detectRegressions(baseline *BenchmarkBaseline, results map[string]*BenchmarkResult, threshold float64) *RegressionReport {
	report := &RegressionReport{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Regressions:     make([]RegressionDetail, 0),
		Warnings:        make([]string, 0),
		PassedThreshold: true,
	}

	// Build reverse mapping from baseline names to parsed results
	baselineToResult := make(map[string]map[string]*BenchmarkResult)
	for benchName, result := range results {
		// Try to find mapping for this benchmark
		if mapping, ok := BenchmarkNameMapping[benchName]; ok {
			category := mapping[0]
			baselineName := mapping[1]
			if baselineToResult[category] == nil {
				baselineToResult[category] = make(map[string]*BenchmarkResult)
			}
			baselineToResult[category][baselineName] = result
		}
	}

	// Check each baseline against results
	for category, benchmarks := range baseline.Benchmarks {
		for name, metric := range benchmarks {
			fullName := category + "." + name

			// Check if we have a result for this benchmark
			result, ok := baselineToResult[category][name]
			if !ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("No result found for %s", fullName))
				continue
			}

			report.TotalBench++

			// Convert ns to ms
			actualMs := result.NsPerOp / 1_000_000
			thresholdMs := metric.ThresholdMs

			// Calculate degradation percentage
			if thresholdMs > 0 {
				degradation := float64(actualMs-thresholdMs) / float64(thresholdMs) * 100

				// Check if regression exceeds threshold
				if degradation > threshold {
					report.RegressionCount++
					report.PassedThreshold = false

					report.Regressions = append(report.Regressions, RegressionDetail{
						Benchmark:      fullName,
						ThresholdMs:    thresholdMs,
						ActualMs:       actualMs,
						DegradationPct: math.Round(degradation*100) / 100,
						Status:         "FAIL",
					})
				}
			}
		}
	}

	// Generate summary
	if report.PassedThreshold {
		report.Summary = fmt.Sprintf("✅ All %d benchmarks passed performance thresholds", report.TotalBench)
	} else {
		report.Summary = fmt.Sprintf("❌ %d/%d benchmarks exceeded performance thresholds (regression >%.1f%%)",
			report.RegressionCount, report.TotalBench, threshold)
	}

	return report
}

func outputReport(report *RegressionReport, outputPath string) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal report: %v", err)
	}

	// Print to stdout
	fmt.Println(string(data))

	// Write to file if specified
	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0644); err != nil {
			log.Printf("Warning: failed to write report to %s: %v", outputPath, err)
		}
	}

	// Print summary to stderr for visibility
	fmt.Fprintf(os.Stderr, "\n%s\n", report.Summary)

	if len(report.Regressions) > 0 {
		fmt.Fprintf(os.Stderr, "\nRegressions detected:\n")
		for _, reg := range report.Regressions {
			fmt.Fprintf(os.Stderr, "  ❌ %s: %.1f%% slower (threshold: %dms, actual: %dms)\n",
				reg.Benchmark, reg.DegradationPct, reg.ThresholdMs, reg.ActualMs)
		}
	}

	if len(report.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "\nWarnings:\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s\n", warning)
		}
	}
}
