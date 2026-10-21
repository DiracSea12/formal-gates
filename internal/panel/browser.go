// Managed browser launching for panel windows. Only executables installed on
// the machine are used, each window gets its own profile directory so closing
// it kills exactly its own browser process, and no debugger flags or detached
// OS URL openers are ever used — a launch either produces a process the
// caller owns or reports the real reason none exists.
package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Browser reports one usable managed-browser installation. Kind is "app" for
// Chromium-family app-mode windows or "firefox" for the plain-window fallback.
type Browser struct {
	Path string
	Kind string
}

// ErrBrowserUnavailable reports that no managed browser or desktop exists.
type ErrBrowserUnavailable struct{ Reason string }

func (e *ErrBrowserUnavailable) Error() string { return "managed browser unavailable: " + e.Reason }

// desktopAvailable reports a graphical session on platforms that need one.
func desktopAvailable() bool {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default:
		return true
	}
}

func firstExisting(paths []string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func chromiumPaths() []string {
	switch runtime.GOOS {
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LocalAppData")
		return []string{
			filepath.Join(programFiles, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(programFilesX86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(localAppData, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(programFiles, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(programFilesX86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(localAppData, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(programFiles, `Chromium\Application\chrome.exe`),
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
			if path, err := exec.LookPath(name); err == nil {
				return []string{path}
			}
		}
		return nil
	}
}

func firefoxPaths() []string {
	switch runtime.GOOS {
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		return []string{
			filepath.Join(programFiles, `Mozilla Firefox\firefox.exe`),
			filepath.Join(programFilesX86, `Mozilla Firefox\firefox.exe`),
		}
	case "darwin":
		return []string{"/Applications/Firefox.app/Contents/MacOS/firefox"}
	default:
		if path, err := exec.LookPath("firefox"); err == nil {
			return []string{path}
		}
		return nil
	}
}

// FindBrowser locates a browser that supports a managed window. Chromium
// app-mode is preferred; Firefox runs as an ordinary window on its own
// profile. With no usable browser — or no desktop session — the error names
// the real reason instead of pretending a launch.
func FindBrowser() (Browser, error) {
	if !desktopAvailable() {
		return Browser{}, &ErrBrowserUnavailable{Reason: "no desktop session (DISPLAY and WAYLAND_DISPLAY are unset)"}
	}
	if path := firstExisting(chromiumPaths()); path != "" {
		return Browser{Path: path, Kind: "app"}, nil
	}
	if path := firstExisting(firefoxPaths()); path != "" {
		return Browser{Path: path, Kind: "firefox"}, nil
	}
	return Browser{}, &ErrBrowserUnavailable{Reason: "no installed Chrome, Edge, Chromium, or Firefox found"}
}

// LaunchBrowser returns a command that opens url in a managed window on the
// caller-provided profile directory. Starting and owning it — job-object or
// process-group lifetime — is the caller's job via harness.StartOwnedProcess.
func (b Browser) LaunchBrowser(url, profileDir string) (*exec.Cmd, error) {
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, err
	}
	switch b.Kind {
	case "app":
		// The served page's heartbeat timer keeps the window alive, so its
		// timers and renderer must keep running while the window is occluded
		// or behind another window.
		return exec.Command(b.Path, "--app="+url, "--user-data-dir="+profileDir,
			"--no-first-run", "--no-default-browser-check",
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding"), nil
	case "firefox":
		return exec.Command(b.Path, "--no-remote", "--profile", profileDir, "--new-window", url), nil
	default:
		return nil, &ErrBrowserUnavailable{Reason: "unknown browser kind " + b.Kind}
	}
}

// BrowserProfileDir is the per-project, per-window managed profile base under
// the user cache; each distinct window gets its own subdirectory so browsers
// never share a profile's single-instance forwarding.
func BrowserProfileDir(root, windowID string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(cache, "formal-gates", "panel-browser", hex.EncodeToString(sum[:8]), windowID), nil
}
