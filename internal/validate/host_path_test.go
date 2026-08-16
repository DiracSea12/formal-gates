package validate

import (
	"runtime"
	"testing"
)

func TestNormalizeHostPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("POSIX drive normalization is Windows-only")
	}
	for _, tc := range []struct{ in, want string }{
		{"/c/Users/x/y.go", "c:/Users/x/y.go"},
		{"/C/Users/x", "C:/Users/x"},
		{"relative/path.go", "relative/path.go"},
		{"/no-drive-letter", "/no-drive-letter"},
		{"C:/already/windows.go", "C:/already/windows.go"},
	} {
		if got := normalizeHostPath(tc.in); got != tc.want {
			t.Errorf("normalizeHostPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHostMatcher(t *testing.T) {
	if got := hostMatcher("codex"); got != ".*" {
		t.Errorf("codex matcher should be regex .*, got %q", got)
	}
	for _, host := range []string{"claude", "cursor"} {
		if got := hostMatcher(host); got != "*" {
			t.Errorf("%s matcher should be glob *, got %q", host, got)
		}
	}
}
