package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcus/sidecar/internal/docdrift"
)

func main() {
	var (
		projectRoot  = flag.String("project", ".", "Project root directory")
		outputFormat = flag.String("format", "text", "Output format: text, json, markdown")
		showVersion  = flag.Bool("version", false, "Show version")
	)

	flag.Parse()

	if *showVersion {
		fmt.Println("doccheck v1.0.0")
		os.Exit(0)
	}

	// Resolve absolute path
	absPath, err := filepath.Abs(*projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving project path: %v\n", err)
		os.Exit(1)
	}

	// Verify project root exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Project root does not exist: %s\n", absPath)
		os.Exit(1)
	}

	// Validate format
	var format docdrift.ReportFormat
	switch *outputFormat {
	case "json":
		format = docdrift.FormatJSON
	case "markdown":
		format = docdrift.FormatMarkdown
	default:
		format = docdrift.FormatText
	}

	// Create and run detector
	config := docdrift.Config{
		ProjectRoot:  absPath,
		OutputFormat: format,
	}

	detector := docdrift.NewDetector(config)
	if err := detector.Detect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running detector: %v\n", err)
		os.Exit(1)
	}

	// Print report
	if detector.Report != nil {
		fmt.Print(detector.GetFormattedReport())

		// Exit with non-zero if gaps found
		if len(detector.Report.Gaps) > 0 {
			os.Exit(1)
		}
	}
}
