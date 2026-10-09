package main

import (
	"os"

	"formal-gates/internal/cli"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
)

func main() {
	facade.NativeSourceVerifier = harness.NativeResultEvidence
	if len(os.Args) > 1 && os.Args[1] == "harness" {
		os.Exit(harness.RunCommand(os.Args[2:], os.Stdin, os.Stdout, os.Stderr, facade.AdmitExecution))
	}
	code := cli.Run("formal-gates", os.Args[1:], cli.IO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if code != 0 {
		os.Exit(code)
	}
}
