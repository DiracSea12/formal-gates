// Command sqlprobe runs the G0 SQLite driver compatibility probe.
//
// Usage:
//
//	go run ./scripts/sqlprobe -dir <isolated-directory>
//
// It writes the machine-readable report to stdout and to
// <dir>/probe-report.json. It exits non-zero when any check fails, so an
// acceptance runner can treat the exit code as the check result.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir := flag.String("dir", "", "isolated directory for probe databases (required)")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "sqlprobe: -dir is required")
		os.Exit(2)
	}
	report, err := Run(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sqlprobe: %v\n", err)
		os.Exit(2)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sqlprobe: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(string(encoded))
	if err := os.WriteFile(filepath.Join(report.Dir, "probe-report.json"), append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sqlprobe: %v\n", err)
		os.Exit(2)
	}
	if !report.OK {
		fmt.Fprintf(os.Stderr, "sqlprobe: %d check(s) failed: %v\n", len(report.Failed), report.Failed)
		os.Exit(1)
	}
	os.Exit(0)
}
