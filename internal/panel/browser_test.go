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
// window as timeout, killing its owned browser, and removing its profile.
func TestManagedBrowserIdleTimeout(t *testing.T) {
	if _, err := FindBrowser(); err != nil {
		t.Skipf("no managed browser on this host: %v", err)
	}
	home := panelHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"panelIdleTimeout":3}`), 0o600); err != nil {
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

	// The served page must actually heartbeat — proof the browser loaded it.
	deadline := time.Now().Add(20 * time.Second)
	for {
		server.mu.Lock()
		seen := !session.seen.IsZero()
		server.mu.Unlock()
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("served page never heartbeat — window is not a live UI")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Heartbeats keep the window alive but are not user activity: after the
	// idle timeout the service resolves the window and kills its browser.
	deadline = time.Now().Add(30 * time.Second)
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
