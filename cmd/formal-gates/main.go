package main

import (
	"os"

	"formal-gates/internal/cli"
	"formal-gates/internal/dogfood"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "dogfood" {
		os.Exit(dogfood.RunCommand(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
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
