package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
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

func samePath(a, b string) bool {
	a, b = absPath(a), absPath(b)
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func cleanWorktree(worktree string) string { return absPath(lifecycle.CleanRoot(worktree)) }

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
