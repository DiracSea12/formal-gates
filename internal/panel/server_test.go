package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/settings"
)

// fakeClock drives Server.sweep deterministically; Serve's own ticker reads
// the same s.now concurrently, so the field is mutex-guarded.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func startPanelServer(t *testing.T, b Backend) (*Server, *fakeClock, context.CancelFunc) {
	t.Helper()
	server, err := NewServer(b)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Now()}
	server.now = clock.now
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("server did not stop")
		}
	})
	return server, clock, cancel
}

func panelGet(t *testing.T, server *Server, path string) *http.Response {
	t.Helper()
	response, err := http.Get("http://" + server.Addr() + path)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func serverPost(t *testing.T, server *Server, path, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest("POST", "http://"+server.Addr()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://"+server.Addr())
	request.Header.Set("X-Formal-Gates-Token", server.token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func bodyJSON(t *testing.T, r *http.Response) map[string]any {
	t.Helper()
	defer r.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func intakeFixture(t *testing.T, root string) string {
	t.Helper()
	putJSON(t, filepath.Join(root, "requirements.md"), panelRequirement)
	candidate := filepath.Join(root, "intake-candidate.json")
	data, _ := json.Marshal(panelReceipt(root, "requirements.md"))
	putJSON(t, candidate, string(data))
	return candidate
}

func selectionConfig(t *testing.T, root, home, name string) {
	t.Helper()
	cfg := settings.Configuration{FlowSelection: &settings.FlowSelection{Disabled: []string{"repair"}}}
	if _, err := settings.WriteConfiguration(root, home, "project", name, cfg, false, nil); err != nil {
		t.Fatal(err)
	}
}

func TestServerServesPageAssetsAndBootstrapUnderHostGuard(t *testing.T) {
	b := Backend{Root: t.TempDir(), Home: panelHome(t)}
	server, _, _ := startPanelServer(t, b)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("GET", window.URL, nil)
	request.Host = "elsewhere.test"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host page = %d", response.StatusCode)
	}
	response, err = http.Get(window.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), window.ID) || !strings.Contains(string(body), server.token) {
		t.Fatalf("page missing escaped bootstrap: %d", response.StatusCode)
	}
	for _, asset := range []string{"/app.js", "/style.css"} {
		response, err = http.Get("http://" + server.Addr() + asset)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", asset, response.StatusCode)
		}
	}
	response = panelGet(t, server, "/?window=nosuchwindow")
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown window page = %d", response.StatusCode)
	}
}

func TestServerWindowWritesNeedOriginAndToken(t *testing.T) {
	b := Backend{Root: t.TempDir(), Home: panelHome(t)}
	server, _, _ := startPanelServer(t, b)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"window":%q,"activity":true}`, window.ID)
	for _, path := range []string{"/api/window/heartbeat", "/api/window/close", "/api/window/select"} {
		request, _ := http.NewRequest("POST", "http://"+server.Addr()+path, strings.NewReader(body))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s without origin/token = %d", path, response.StatusCode)
		}
	}
	response := serverPost(t, server, "/api/window/heartbeat", body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid heartbeat = %d", response.StatusCode)
	}
}

func TestServerIntakeSelectResolvesCallerAndClosesWindow(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "面板")
	server, _, _ := startPanelServer(t, Backend{Root: root, Home: home})
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	response := serverPost(t, server, "/api/window/select", fmt.Sprintf(`{"window":%q,"configuration":"面板"}`, window.ID))
	out := bodyJSON(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("select = %d %v", response.StatusCode, out)
	}
	select {
	case <-window.Done:
	default:
		t.Fatal("select did not resolve the waiter")
	}
	result := window.Result()
	if result.Status != "selected" || result.Configuration != "面板" || len(result.Disabled) == 0 {
		t.Fatalf("result = %+v", result)
	}
	closed := false
	if err := server.AttachWindowCloser(window.ID, func() error { closed = true; return nil }); err != nil || !closed {
		t.Fatalf("attach after close = %v closed=%v", err, closed)
	}
	response = serverPost(t, server, "/api/window/select", fmt.Sprintf(`{"window":%q,"configuration":"面板"}`, window.ID))
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("select on a closed window was accepted")
	}
}

func TestServerSupersededIntakeResolvesButKeepsWindowOpen(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "面板")
	server, _, _ := startPanelServer(t, Backend{Root: root, Home: home})
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	serverPost(t, server, "/api/window/heartbeat", fmt.Sprintf(`{"window":%q}`, window.ID)).Body.Close()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte("# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.sweep()
	<-window.Done
	result := window.Result()
	if result.Status != "superseded" {
		t.Fatalf("result = %+v", result)
	}
	response := panelGet(t, server, "/api/window?window="+window.ID)
	out := bodyJSON(t, response)
	if out["open"] != true || out["mode"] != "library" {
		t.Fatalf("superseded window = %v", out)
	}
}

func TestServerWindowReuseAndDistinctPaths(t *testing.T) {
	b := Backend{Root: t.TempDir(), Home: panelHome(t)}
	server, _, _ := startPanelServer(t, b)
	first, _ := server.NewWindow("/abs/intake-a.json")
	again, _ := server.NewWindow("/abs/intake-a.json")
	if again.ID != first.ID {
		t.Fatal("same intake path did not reuse the live window")
	}
	other, _ := server.NewWindow("/abs/intake-b.json")
	if other.ID == first.ID {
		t.Fatal("different intake path reused the window")
	}
	free, _ := server.NewWindow("")
	free2, _ := server.NewWindow("")
	if free.ID == first.ID || free.ID == other.ID || free2.ID == free.ID {
		t.Fatal("empty intake path did not create distinct library windows")
	}
}

func TestServerHeartbeatLossCancelsIntakeAndHeartbeatIsNotActivity(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, userConfigPath(home), `{"panelIdleTimeout":60}`)
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "面板")
	if _, err := settings.UseConfiguration(root, home, "project", "面板", nil); err != nil {
		t.Fatal(err)
	}
	server, clock, _ := startPanelServer(t, Backend{Root: root, Home: home})

	// Heartbeats alone keep the connection alive but never count as user
	// activity: the idle timeout still fires with the active-pointer result.
	window, _ := server.NewWindow(candidate)
	for i := 0; i < 5; i++ {
		clock.advance(10 * time.Second)
		serverPost(t, server, "/api/window/heartbeat", fmt.Sprintf(`{"window":%q}`, window.ID)).Body.Close()
	}
	clock.advance(11 * time.Second) // idle 61s > 60s, heartbeat age 11s < 15s
	server.sweep()
	<-window.Done
	result := window.Result()
	if result.Status != "timeout" || result.Configuration != "面板" {
		t.Fatalf("timeout result = %+v", result)
	}

	// Real activity resets the idle clock; losing the heartbeat afterwards
	// still cancels the intake waiter.
	second, _ := server.NewWindow(candidate)
	clock.advance(10 * time.Second)
	serverPost(t, server, "/api/window/heartbeat", fmt.Sprintf(`{"window":%q,"activity":true}`, second.ID)).Body.Close()
	clock.advance(10 * time.Second)
	serverPost(t, server, "/api/window/heartbeat", fmt.Sprintf(`{"window":%q}`, second.ID)).Body.Close()
	clock.advance(10 * time.Second)
	server.sweep()
	select {
	case <-second.Done:
		t.Fatal("recent user activity window closed early")
	default:
	}
	clock.advance(20 * time.Second) // heartbeat age 40s > 15s
	server.sweep()
	<-second.Done
	if r := second.Result(); r.Status != "cancelled" {
		t.Fatalf("heartbeat-loss result = %+v", r)
	}
}

func TestServerZeroIdleDisablesUserTimeoutOnly(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, userConfigPath(home), `{"panelIdleTimeout":0}`)
	server, clock, _ := startPanelServer(t, Backend{Root: root, Home: home})
	window, _ := server.NewWindow("")
	for i := 0; i < 5; i++ {
		clock.advance(10 * time.Second)
		serverPost(t, server, "/api/window/heartbeat", fmt.Sprintf(`{"window":%q}`, window.ID)).Body.Close()
	}
	server.sweep()
	response := panelGet(t, server, "/api/window?window="+window.ID)
	out := bodyJSON(t, response)
	if out["open"] != true {
		t.Fatal("panelIdleTimeout=0 closed an idle window")
	}
	clock.advance(20 * time.Second)
	server.sweep()
	<-window.Done
	if r := window.Result(); r.Status != "closed" {
		t.Fatalf("library heartbeat-loss result = %+v", r)
	}
}

func TestServerCloseEndpointCancelsIntakeWaiter(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	candidate := intakeFixture(t, root)
	server, _, _ := startPanelServer(t, Backend{Root: root, Home: home})
	window, _ := server.NewWindow(candidate)
	serverPost(t, server, "/api/window/close", fmt.Sprintf(`{"window":%q}`, window.ID)).Body.Close()
	<-window.Done
	if r := window.Result(); r.Status != "cancelled" {
		t.Fatalf("close result = %+v", r)
	}
}

func TestServerContextShutdownClosesWindowsAndListener(t *testing.T) {
	b := Backend{Root: t.TempDir(), Home: panelHome(t)}
	server, err := NewServer(b)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Now()}
	server.now = clock.now
	window, _ := server.NewWindow("")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	cancel()
	if err := <-done; err == nil {
		t.Fatal("serve returned nil on context cancel")
	}
	<-window.Done
	if r := window.Result(); r.Status != "closed" {
		t.Fatalf("shutdown result = %+v", r)
	}
	response, err := http.Get(window.URL)
	if err == nil {
		response.Body.Close()
		t.Fatal("listener survived shutdown")
	}
}

// TestPanelPreviewServer is a manual launcher for local browser rendering
// checks: PANEL_PREVIEW=1 go test ./internal/panel -run TestPanelPreviewServer
// prints window URLs and blocks until the test is killed.
func TestPanelPreviewServer(t *testing.T) {
	if os.Getenv("PANEL_PREVIEW") == "" {
		t.Skip("preview launcher")
	}
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	candidate := intakeFixture(t, root)
	selectionConfig(t, root, home, "面板")
	server, err := NewServer(Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	library, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	intake, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("library %s", library.URL)
	t.Logf("intake %s", intake.URL)
	t.Logf("snapshot dir %s", root)
	fmt.Println("PREVIEW_LIBRARY=" + library.URL)
	fmt.Println("PREVIEW_INTAKE=" + intake.URL)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServerClientAbortLeavesWindowOpen(t *testing.T) {
	b := Backend{Root: t.TempDir(), Home: panelHome(t)}
	server, _, _ := startPanelServer(t, b)
	window, _ := server.NewWindow("")
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, "GET", window.URL, nil)
	cancel()
	if _, err := http.DefaultClient.Do(request); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	response := panelGet(t, server, "/api/window?window="+window.ID)
	out := bodyJSON(t, response)
	if out["open"] != true {
		t.Fatal("a dropped client connection closed the window")
	}
}
