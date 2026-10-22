package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/harness"
)

// TestManagedBrowserWindowsAreIndependent launches two real managed browser
// windows on separate profiles, kills the first, and proves the second is
// untouched — the per-window profile and per-window owned process contract.
func TestManagedBrowserWindowsAreIndependent(t *testing.T) {
	visualPanelAvailable(t)
	browser, err := FindBrowser()
	if err != nil {
		t.Skipf("no managed browser on this host: %v", err)
	}
	launch := func(name string) *harness.OwnedProcess {
		profile := filepath.Join(t.TempDir(), name)
		cmd, err := browser.LaunchBrowser("about:blank", profile)
		if err != nil {
			t.Fatalf("launch command %s: %v", name, err)
		}
		proc, err := harness.StartOwnedProcess(cmd)
		if err != nil {
			t.Fatalf("start browser %s: %v", name, err)
		}
		return proc
	}
	first, second := launch("one"), launch("two")
	defer first.Stop()
	defer second.Stop()

	waitAlive := func(pid int) bool {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if processAlive(pid) {
				return true
			}
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}
	if !waitAlive(first.Cmd.Process.Pid) || !waitAlive(second.Cmd.Process.Pid) {
		t.Fatalf("browsers did not start: first alive=%v second alive=%v",
			processAlive(first.Cmd.Process.Pid), processAlive(second.Cmd.Process.Pid))
	}
	// Stop reports the killed browser's nonzero exit; death itself is the
	// invariant asserted below.
	_ = first.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && processAlive(first.Cmd.Process.Pid) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(first.Cmd.Process.Pid) {
		t.Fatal("first window browser survived its own stop")
	}
	if !processAlive(second.Cmd.Process.Pid) {
		t.Fatal("second window browser died when the first window closed")
	}
}

// processAlive answers whether pid names a live process without killing or
// attaching to it.
func processAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
		return err == nil && strings.Contains(string(out), strconv.Itoa(pid))
	}
	proc, err := os.FindProcess(pid)
	return err == nil && proc.Signal(syscall.Signal(0)) == nil
}

// TestManagedBrowserIdleTimeout: a real browser window that only heartbeats —
// no user activity — must hit the configured panelIdleTimeout, resolving the
// window as timeout, killing its owned browser, and removing its profile. The
// idle budget leaves room for a cold browser start and first page load on a
// loaded CI runner, which must not race the timeout.
func TestManagedBrowserIdleTimeout(t *testing.T) {
	visualPanelAvailable(t)
	browser, err := FindBrowser()
	if err != nil {
		t.Skipf("no managed browser on this host: %v", err)
	}
	t.Logf("managed browser: path=%q kind=%q", browser.Path, browser.Kind)
	home := panelHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"panelIdleTimeout":60}`), 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	serveHandle(t, handle)

	response := instanceClient(t, handle.Instance, "POST", "/api/windows/open", `{"intake":""}`)
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("open window: %d %s", response.StatusCode, body)
	}
	var opened struct {
		Window string `json:"window"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&opened); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	t.Logf("window URL: %s", opened.URL)

	server := handle.Server
	server.mu.Lock()
	session := server.windows[opened.Window]
	server.mu.Unlock()
	if session == nil {
		t.Fatal("window did not register its session")
	}
	profile, err := BrowserProfileDir(root, opened.Window)
	if err != nil {
		t.Fatal(err)
	}

	// Park the window state in the log at ~3 s and ~10 s, before the 30 s
	// first-heartbeat grace can close it: the lines distinguish a browser that
	// never loads from one that dies immediately. Logs only; the budget and the
	// assertions below are unchanged.
	diagnosticsCtx, stopDiagnostics := context.WithCancel(context.Background())
	diagnosticsDone := make(chan struct{})
	t.Cleanup(func() {
		stopDiagnostics()
		<-diagnosticsDone
	})
	go func() {
		defer close(diagnosticsDone)
		start := time.Now()
		for _, at := range []time.Duration{3 * time.Second, 10 * time.Second} {
			select {
			case <-diagnosticsCtx.Done():
				return
			case <-time.After(time.Until(start.Add(at))):
			}
			server.mu.Lock()
			seen := session.seen
			open := session.open
			server.mu.Unlock()
			probeCtx, cancel := context.WithTimeout(diagnosticsCtx, 10*time.Second)
			count, lines, listErr := browserProcesses(probeCtx, browser.Path)
			cancel()
			t.Logf("idle timeout diagnostic at %s: open=%v seen.IsZero=%v browser=%s liveProcesses=%d listErr=%v",
				at, open, seen.IsZero(), filepath.Base(browser.Path), count, listErr)
			for _, line := range lines {
				t.Logf("  %.300s", line)
			}
		}
	}()

	// The served page must actually heartbeat — proof the browser loaded it.
	deadline := time.Now().Add(60 * time.Second)
	for {
		server.mu.Lock()
		seen := session.seen
		activity := session.activity
		open := session.open
		server.mu.Unlock()
		if !seen.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Logf("no heartbeat: seen.IsZero()=%v activity=%s open=%v", seen.IsZero(), activity, open)
			logIdleTimeoutDiagnostics(t, browser, opened.URL, profile)
			t.Fatal("served page never heartbeat — window is not a live UI")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Heartbeats keep the window alive but are not user activity: after the
	// idle timeout the service resolves the window and kills its browser.
	deadline = time.Now().Add(90 * time.Second)
	for {
		server.mu.Lock()
		open := session.open
		var result Result
		if session.result != nil {
			result = *session.result
		}
		server.mu.Unlock()
		if !open {
			// A library-mode window resolves as closed when its idle timeout
			// fires; intake-mode windows resolve as timeout.
			if result.Status != "closed" {
				t.Fatalf("idle library window resolved as %q, want closed", result.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle window never timed out")
		}
		time.Sleep(250 * time.Millisecond)
	}
	// The closer joins the browser process before removing its profile, so a
	// vanished profile proves the owned browser tree is dead.
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(profile); os.IsNotExist(err) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("browser profile and process survived the idle timeout")
}

// logIdleTimeoutDiagnostics records what the test process can still observe
// when a managed window never heartbeat: whether the served page answers at
// all, which browser binary is installed, whether a GUI session exists, and
// whether a process of that browser is alive. These are logs only.
func logIdleTimeoutDiagnostics(t *testing.T, browser Browser, url, profile string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A dedicated timeout keeps this probe from hanging CI if the service
	// itself is wedged.
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Logf("page GET: error %v", err)
	} else {
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		t.Logf("page GET: status=%d contentLength=%d bodyBytes=%d readErr=%v",
			response.StatusCode, response.ContentLength, len(body), readErr)
	}
	version, versionErr := exec.CommandContext(ctx, browser.Path, "--version").CombinedOutput()
	t.Logf("browser version: %.200q err=%v", version, versionErr)
	if runtime.GOOS == "darwin" {
		manager, managerErr := exec.CommandContext(ctx, "launchctl", "managername").CombinedOutput()
		t.Logf("launchctl managername: %.200q err=%v", manager, managerErr)
	}
	_, profileErr := os.Stat(profile)
	t.Logf("profile %s: exists=%v err=%v", profile, profileErr == nil, profileErr)
	base := filepath.Base(browser.Path)
	count, lines, listErr := browserProcesses(ctx, browser.Path)
	t.Logf("live processes named %q: %d err=%v", base, count, listErr)
	for _, line := range lines {
		t.Logf("  %.600s", line)
	}
}

// browserProcesses counts the live processes whose image name is the browser
// binary and returns a bounded sample of their command lines: tasklist on
// Windows, ps elsewhere.
func browserProcesses(ctx context.Context, browserPath string) (int, []string, error) {
	base := filepath.Base(browserPath)
	var lines []string
	if runtime.GOOS == "windows" {
		out, err := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq "+base, "/NH").CombinedOutput()
		if err != nil {
			return 0, nil, fmt.Errorf("tasklist: %.600s: %w", out, err)
		}
		// tasklist prints an INFO line instead of rows when nothing matches.
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, base) {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	} else {
		out, err := exec.CommandContext(ctx, "ps", "-Ao", "pid,command").CombinedOutput()
		if err != nil {
			return 0, nil, fmt.Errorf("ps -Ao pid,command: %.200s: %w", out, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, base) {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	count := len(lines)
	if len(lines) > 6 {
		lines = lines[:6]
	}
	return count, lines, nil
}
