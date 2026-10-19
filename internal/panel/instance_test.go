package panel

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/coordination"
)

// serveHandle runs an owned handle's HTTP service until the test ends and
// releases its lock and record afterwards.
func serveHandle(t *testing.T, h *ServiceHandle) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("service did not stop after context cancel")
		}
	})
}

// instanceClient sends a guarded write like a lifecycle delegate does.
func instanceClient(t *testing.T, instance Instance, method, path, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+instance.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = instance.Addr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+instance.Addr)
	request.Header.Set("X-Formal-Gates-Token", instance.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestPanelServiceSingletonOwnership(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	ctx := context.Background()

	owner, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Owned() {
		t.Fatal("first prepare did not own the service")
	}
	serveHandle(t, owner)

	// A second start in the same project delegates to the same port; it must
	// never open a second HTTP listener.
	delegate, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if delegate.Owned() {
		delegate.Release()
		t.Fatal("second prepare claimed ownership while the lock was held")
	}
	if delegate.Instance.Addr != owner.Instance.Addr || delegate.Instance.Token != owner.Instance.Token {
		t.Fatal("delegate received a different instance descriptor")
	}

	// Same intake reuses its window; a different intake opens a second one.
	first, err := delegate.OpenWindow(ctx, "intake-a.json")
	if err != nil {
		t.Fatal(err)
	}
	again, err := delegate.OpenWindow(ctx, "intake-a.json")
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || again.Window != first.Window {
		t.Fatalf("same intake did not reuse its window: %+v then %+v", first, again)
	}
	second, err := delegate.OpenWindow(ctx, "intake-b.json")
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused || second.Window == first.Window {
		t.Fatalf("different intake did not open a distinct window: %+v", second)
	}
	library, err := delegate.OpenWindow(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if library.Reused || library.Window == first.Window {
		t.Fatalf("library open reused an intake window: %+v", library)
	}

	// Closing one window leaves the others open.
	if err := delegate.CloseWindow(ctx, first.Window); err != nil {
		t.Fatal(err)
	}
	open, result, err := delegate.WindowStatus(ctx, second.Window)
	if err != nil || !open || result != nil {
		t.Fatalf("second window lost after closing the first: open=%v result=%+v err=%v", open, result, err)
	}
	open, result, err = delegate.WindowStatus(ctx, first.Window)
	if err != nil || open || result == nil {
		t.Fatalf("closed window did not resolve: open=%v result=%+v err=%v", open, result, err)
	}

	// The instance record exists with owner-private permissions.
	info, err := os.Stat(filepath.Join(root, ".gates", "panel-instance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("instance record permissions = %o", info.Mode().Perm())
	}
}

func TestPanelServiceStaleRecordRecovery(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	record := filepath.Join(root, ".gates", "panel-instance.json")
	if err := os.MkdirAll(filepath.Dir(record), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"pid":1,"addr":"127.0.0.1:1","token":"dead"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// With the lock free, a stale record belongs to a dead owner and is
	// replaced, not trusted.
	handle, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !handle.Owned() {
		t.Fatal("stale record blocked ownership")
	}
	serveHandle(t, handle)
	delegate, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if delegate.Instance.Addr == "127.0.0.1:1" {
		t.Fatal("delegate trusted the stale record")
	}
}

func TestPanelServiceReleaseRemovesRecord(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	handle, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	addr := handle.Instance.Addr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- handle.Serve(ctx) }()
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("serve exit = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "panel-instance.json")); !os.IsNotExist(err) {
		t.Fatalf("instance record survived owner exit: %v", err)
	}
	response, err := http.Get("http://" + addr + "/api/state")
	if err == nil {
		response.Body.Close()
		t.Fatal("listener survived owner exit")
	}
	// The kernel lock is free again: a new owner starts immediately.
	next, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !next.Owned() {
		t.Fatal("service lock was not released with the owner")
	}
	serveHandle(t, next)
}

func TestPanelServiceEmptyStartupGrace(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	handle, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	handle.Server.now = func() time.Time { return time.Now() }
	handle.Server.SetEmptyTimeout(50 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- handle.Serve(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("grace exit = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("windowless service did not exit within its startup grace")
	}
}

func TestPanelWindowOpenRequiresGuards(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	handle, err := PrepareService(context.Background(), Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	serveHandle(t, handle)
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("X-Formal-Gates-Token") },
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Host = "evil.example" },
	} {
		request, err := http.NewRequest("POST", "http://"+handle.Instance.Addr+"/api/windows/open", strings.NewReader(`{"intake":""}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Host = handle.Instance.Addr
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://"+handle.Instance.Addr)
		request.Header.Set("X-Formal-Gates-Token", handle.Instance.Token)
		mutate(request)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("unguarded lifecycle write succeeded: %d", response.StatusCode)
		}
	}
}

// TestWaitOrAcquireTakesOverDeadStarter: a starter that dies holding the
// kernel lock before publishing its record must not strand waiting callers —
// the waiter acquires the same lock and takes over as owner.
func TestWaitOrAcquireTakesOverDeadStarter(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, _ := instancePaths(canonical)
	starter, err := coordination.AcquirePath(lockPath, "dead starter")
	if err != nil {
		t.Fatal(err)
	}
	// A stale half-written record from the dead starter must not be trusted.
	record := filepath.Join(canonical, ".gates", "panel-instance.json")
	if err := os.MkdirAll(filepath.Dir(record), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"pid":1,"addr":"127.0.0.1:1","token":"dead"`), 0o600); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		instance Instance
		unlock   func()
		err      error
	}
	waited := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		instance, unlock, err := WaitOrAcquire(ctx, canonical)
		waited <- outcome{instance, unlock, err}
	}()
	// While the starter's lock is held the waiter can only wait.
	select {
	case got := <-waited:
		got.unlock()
		t.Fatal("waiter returned while the starter still held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	// The starter dies; the waiter must take the lock through the same path.
	starter()
	var got outcome
	select {
	case got = <-waited:
	case <-time.After(15 * time.Second):
		t.Fatal("waiter never took over after the starter died")
	}
	if got.err != nil || got.unlock == nil {
		t.Fatalf("waiter did not acquire the service lock: %v", got.err)
	}
	got.unlock()
}

// TestPanelServiceBadConfigStillIdentifiable: a corrupted project config must
// not masquerade as a dead service — the recorded process still answers its
// guarded identity endpoint and opens windows while the UI reports the bad
// configuration.
func TestPanelServiceBadConfigStillIdentifiable(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	ctx := context.Background()
	owner, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Owned() {
		t.Fatal("first prepare did not own the service")
	}
	serveHandle(t, owner)

	// Corrupt the configuration after the service is already running.
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A delegate still discovers and health-checks the live owner.
	delegate, err := PrepareService(ctx, Backend{Root: root, Home: home})
	if err != nil {
		t.Fatalf("bad config blocked delegate discovery: %v", err)
	}
	if delegate.Owned() || delegate.Instance.Addr != owner.Instance.Addr {
		t.Fatal("delegate did not attach to the live bad-config service")
	}

	// The window endpoint still works — the UI reports and recovers.
	if _, err := FindBrowser(); err != nil {
		t.Skipf("no managed browser on this host: %v", err)
	}
	window, err := delegate.OpenWindow(ctx, "")
	if err != nil {
		t.Fatalf("bad config service could not open a window: %v", err)
	}
	if window.Window == "" || window.URL == "" {
		t.Fatalf("window descriptor incomplete: %+v", window)
	}
}
