package validate

import (
	"os"
	"path/filepath"
	"strings"
)

type Failure struct {
	Path    string
	Message string
}

type Result struct {
	Failures []Failure
}

func (r Result) OK() bool {
	return len(r.Failures) == 0
}

func (r *Result) add(path, message string) {
	r.Failures = append(r.Failures, Failure{Path: slash(path), Message: message})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func slash(path string) string {
	if path == "" {
		return path
	}
	return filepath.ToSlash(path)
}

func cleanRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}
	return root
}
