package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

var valueKey, version string

// The peer binds its native output and usage to the original prompt and identity.
func main() {
	if len(os.Args) != 3 {
		panic("expected prompt and identity")
	}
	prompt, identity := os.Args[1], os.Args[2]
	if identity == "" {
		identity = "peer-session"
	}
	output, err := strconv.Atoi(version)
	if err != nil {
		panic(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"identity": identity, valueKey: prompt, "input": len(prompt), "output": output}); err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "stderr-"+prompt)
}
