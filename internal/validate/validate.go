package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func resolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, filepath.FromSlash(value))
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return slash(path)
	}
	return slash(rel)
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

func cleanWorktree(worktree string) string { return absPath(cleanRoot(worktree)) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64, bool:
		return strings.TrimSpace(fmt.Sprint(v))
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && rv.Kind() >= reflect.Int && rv.Kind() <= reflect.Uint64 {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

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
