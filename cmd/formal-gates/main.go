package main

import (
	"os"

	"formal-gates/internal/cli"
)

func main() {
	code := cli.Run("formal-gates", os.Args[1:], cli.IO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if code != 0 {
		os.Exit(code)
	}
}
