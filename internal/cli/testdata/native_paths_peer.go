package main

import (
	"fmt"
	"os"
	"strings"
)

// This peer records native discovery and refuses supervisor execution before a model call.
func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--help" {
		values := []string{os.Getenv("HOME"), os.Getenv("CODEBUDDY_CONFIG_DIR"), os.Getenv("WORKBUDDY_CONFIG_DIR"), os.Getenv("ACC_PRODUCT_CONFIG_PATH")}
		record("WORKFLOW_NATIVE_CALLS", strings.Join(values, "|"))
		fmt.Println("--resume --session-id stream-json --mcp-config --tools --permission-mode dontAsk")
		return
	}
	if len(args) == 3 && args[0] == "harness" && args[1] == "supervise" {
		record("WORKFLOW_SUPERVISOR_CALLS", args[2])
		os.Exit(93)
	}
	os.Exit(91)
}

func record(key, value string) {
	file, err := os.OpenFile(os.Getenv(key), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	if _, err := fmt.Fprintln(file, value); err != nil {
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
}
