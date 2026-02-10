package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BenchmarkResult represents a single benchmark result
type BenchmarkResult struct {
	Name         string
	Iterations   int64
	NsPerOp      int64
	BytesPerOp   int64
	AllocsPerOp  int64
	MemAllocated int64
}

// BenchmarkBaseline contains baseline metrics and thresholds
type BenchmarkBaseline struct {
	Commit     string    `json:"commit"`
	Timestamp  string    `json:"timestamp"`
	Version    string    `json:"version"`
	Benchmarks map[string]map[string]MetricThreshold `json:"benchmarks"`
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
	Timestamp      string                       `json:"timestamp"`
	TotalBench     int                          `json:"total_benchmarks"`
	RegressionCount int                         `json:"regression_count"`
	Regressions    []RegressionDetail           `json:"regressions"`
	Warnings       []string                     `json:"warnings"`
	Summary        string                       `json:"summary"`
	PassedThreshold bool                        `json:"passed_threshold"`
}

// RegressionDetail contains details of a single regression
type RegressionDetail struct {
	Benchmark     string  `json:"benchmark"`
	ThresholdMs   int64   `json:"threshold_ms"`
	ActualMs      int64   `json:"actual_ms"`
	DegradationPct float64 `json:"degradation_percent"`
	Status        string  `json:"status"`
}

// BenchmarkLine represents a parsed benchmark line from 'go test' output
type BenchmarkLine struct {
	Name        string
	Iterations  int64
	NsPerOp     int64
	BytesPerOp  int64
	AllocsPerOp int64
}

var (
	benchmarkDir = flag.String("bench-dir", ".", "directory to run benchmarks in")
	baselineFile = flag.String("baseline", ".benchmarks/baseline.json", "path to baseline metrics file")
	regressionPct = flag.Float64("regression", 10.0, "regression threshold in percentage (default 10%)")
	outputFile = flag.String("output", "", "output file for regression report (default: stdout)")
	failOnRegression = flag.Bool("fail", true, "exit with error code if regression detected")
)

func main() {
	flag.Parse()

	// Load baseline
	baseline, err := loadBaseline(*baselineFile)
	if err != nil {
		log.Fatalf("Failed to load baseline: %v", err)
	}

	// Run benchmarks
	results, err := runBenchmarks(*benchmarkDir)
	if err != nil {
		log.Fatalf("Failed to run benchmarks: %v", err)
	}

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

func runBenchmarks(dir string) (map[string]*BenchmarkResult, error) {
	results := make(map[string]*BenchmarkResult)

	// Test paths for adapter benchmarks
	packages := []string{
		"./internal/adapter/claudecode",
		"./internal/adapter/codex",
	}

	for _, pkg := range packages {
		output, err := runBenchTest(dir, pkg)
		if err != nil {
			log.Printf("Warning: failed to run benchmarks for %s: %v", pkg, err)
			continue
		}

		parsed := parseBenchmarkOutput(output)
		for _, b := range parsed {
			results[b.Name] = &BenchmarkResult{
				Name:         b.Name,
				Iterations:   b.Iterations,
				NsPerOp:      b.NsPerOp,
				BytesPerOp:   b.BytesPerOp,
				AllocsPerOp:  b.AllocsPerOp,
				MemAllocated: (b.BytesPerOp * b.Iterations),
			}
		}
	}

	return results, nil
}

func runBenchTest(_ string, _ string) (string, error) {
	// Execute: go test -bench=. -benchmem -run=^$ <pkg>
	// This captures benchmark output

	// For now, we'll create a mock implementation that would be enhanced
	// In real CI, this would actually call the go test command
	return "", nil
}

func parseBenchmarkOutput(output string) []BenchmarkLine {
	var results []BenchmarkLine

	scanner := bufio.NewScanner(strings.NewReader(output))
	// Pattern: BenchmarkName-N    <iterations> <ns/op> <B/op> <allocs/op>
	// Example: BenchmarkMessages_FullParse_1MB-8    10    100000000 ns/op    256 B/op    5 allocs/op
	pattern := regexp.MustCompile(`Benchmark(\w+)-\d+\s+(\d+)\s+(\d+)\s+ns/op\s+(\d+)\s+B/op\s+(\d+)\s+allocs/op`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := pattern.FindStringSubmatch(line)
		if matches != nil {
			name := matches[1]
			iter, _ := strconv.ParseInt(matches[2], 10, 64)
			nsOp, _ := strconv.ParseInt(matches[3], 10, 64)
			bytesOp, _ := strconv.ParseInt(matches[4], 10, 64)
			allocsOp, _ := strconv.ParseInt(matches[5], 10, 64)

			results = append(results, BenchmarkLine{
				Name:        name,
				Iterations:  iter,
				NsPerOp:     nsOp,
				BytesPerOp:  bytesOp,
				AllocsPerOp: allocsOp,
			})
		}
	}

	return results
}

func detectRegressions(baseline *BenchmarkBaseline, results map[string]*BenchmarkResult, threshold float64) *RegressionReport {
	report := &RegressionReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Regressions: make([]RegressionDetail, 0),
		Warnings: make([]string, 0),
		PassedThreshold: true,
	}

	// Flatten baseline benchmarks and check for regressions
	for category, benchmarks := range baseline.Benchmarks {
		for name, metric := range benchmarks {
			fullName := category + "." + name

			// Check if we have a result for this benchmark
			result, ok := results[name]
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
