package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This test package owns a minimal installable package contract and implements
// the same validation and version protocols as a release binary.
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("fixture-package-owner")
		return
	}
	if len(os.Args) != 5 || os.Args[1] != "package" || os.Args[2] != "validate" || os.Args[3] != "--root" {
		fail(fmt.Errorf("unsupported fixture protocol: %v", os.Args[1:]))
	}
	root := os.Args[4]
	if logPath := os.Getenv("PACKAGE_OWNER_VALIDATION_LOG"); logPath != "" {
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fail(err)
		}
		_, err = fmt.Fprintln(file, root)
		file.Close()
		if err != nil {
			fail(err)
		}
	}
	dir, err := os.Getwd()
	if err != nil || dir != root {
		fail(fmt.Errorf("package root and cwd differ: %s %s %v", root, dir, err))
	}
	binary, err := os.Executable()
	if err != nil || filepath.Join(root, "bin", filepath.Base(binary)) != binary {
		fail(fmt.Errorf("package binary differs from root: %s %s %v", binary, root, err))
	}
	data, err := os.ReadFile(filepath.Join(root, "formal-gates.manifest.json"))
	if err != nil {
		fail(err)
	}
	var doc struct {
		Name  string   `json:"name"`
		Parts []string `json:"package_parts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		fail(err)
	}
	if doc.Name != "formal-gates" {
		fail(fmt.Errorf("unexpected package name: %s", doc.Name))
	}
	for _, part := range append([]string{"SKILL.md", "formal-gates.manifest.json", "bin/" + filepath.Base(binary)}, doc.Parts...) {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(part, "/"))))
		if err != nil {
			fail(err)
		}
		if strings.HasSuffix(part, "/") && !info.IsDir() {
			fail(fmt.Errorf("declared directory is a file: %s", part))
		}
	}
	fmt.Println("PASS")
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
