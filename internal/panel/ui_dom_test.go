package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

// panelBrowser locates an installed Chromium-family browser for headless DOM
// driving; nothing is downloaded. The installed-path order prefers Edge, then
// Chrome, because Chrome refuses an ephemeral DevTools browser under the
// redirected USERPROFILE that the DOM fixtures set. The PATH lookup prefers
// chrome, then msedge, then chromium.
func panelBrowser(t *testing.T) string {
	t.Helper()
	visualPanelAvailable(t)
	candidates := []string{
		`C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe`,
		`C:/Program Files/Google/Chrome/Application/chrome.exe`,
		`C:/Program Files (x86)/Google/Chrome/Application/chrome.exe`,
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	for _, name := range []string{"chrome", "msedge", "chromium"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no installed chromium browser")
	return ""
}

// visualPanelAvailable skips a test that needs a real browser window on hosts
// without an interactive desktop: headless Linux has no DISPLAY/WAYLAND
// session, and the hosted CI runners never present the managed browser page.
// Developers' desktops run these tests by default; FORMAL_GATES_VISUAL_PANEL=1
// forces them where they are otherwise skipped.
func visualPanelAvailable(t *testing.T) {
	t.Helper()
	if os.Getenv("FORMAL_GATES_VISUAL_PANEL") == "1" {
		return
	}
	if !desktopAvailable() {
		t.Skip("no desktop session for a visual panel (DISPLAY and WAYLAND_DISPLAY are unset)")
	}
	if os.Getenv("CI") != "" {
		t.Skip("hosted CI runner has no interactive desktop for a visual panel; set FORMAL_GATES_VISUAL_PANEL=1 to force")
	}
}

// domDriverMiddleware appends the testdata driver to the served app.js and
// exposes /__ready so the page can signal the test without any production
// code path knowing about tests.
func domDriverMiddleware(t *testing.T, ready chan<- struct{}) func(http.Handler) http.Handler {
	t.Helper()
	app, err := ui.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join("testdata", "dom_driver.js"))
	if err != nil {
		t.Fatal(err)
	}
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Write(app)
			w.Write(driver)
		})
		mux.HandleFunc("POST /__ready", func(w http.ResponseWriter, r *http.Request) {
			select {
			case ready <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.Handle("/", next)
		return mux
	}
}

// runDOM launches the installed browser headless against a window URL and
// returns the dumped DOM; --virtual-time-budget lets page timers run to
// completion quickly.
func runDOM(t *testing.T, browser, url string, args ...string) string {
	t.Helper()
	profile := t.TempDir()
	// CI Linux runners restrict unprivileged user namespaces, so the headless
	// test browser runs without the OS sandbox; it only ever loads the locally
	// served test page. Container runners mount a small /dev/shm, so
	// --disable-dev-shm-usage keeps Chromium on temp-file shared memory instead
	// of exhausting that mount.
	cmdArgs := append([]string{"--headless=new", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu", "--no-first-run", "--disable-extensions",
		"--user-data-dir=" + profile, "--virtual-time-budget=240000", "--dump-dom"}, args...)
	cmdArgs = append(cmdArgs, url)
	// The virtual-time dump of the heavier probes legitimately takes minutes on
	// a loaded runner; a genuinely stuck browser still fails at the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, browser, cmdArgs...).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("browser timed out; partial dump is not evidence:\n%.20000s", out)
	}
	if err != nil {
		if dumpedDOMComplete(out) {
			t.Logf("headless browser exited without a clean shutdown after a complete dump: %v", err)
			return string(out)
		}
		t.Fatalf("browser run failed without a complete dump: %v bytes=%d\n%.20000s", err, len(out), out)
	}
	return string(out)
}

// dumpedDOMComplete reports whether the browser printed the driver's finished
// result block, i.e. the dump is evidence even if the headless process aborted
// while shutting down.
func dumpedDOMComplete(dump []byte) bool {
	i := bytes.Index(dump, []byte("RESULT"))
	return i >= 0 && bytes.Contains(dump[i:], []byte("END"))
}

func domResults(t *testing.T, dump string) {
	t.Helper()
	marker := "RESULT"
	i := strings.Index(dump, marker)
	if i < 0 {
		t.Fatalf("driver produced no result block; tail of dump:\n%.1500s", dump[max(0, len(dump)-4000):])
	}
	if !strings.Contains(dump[i:], "END") {
		t.Fatalf("driver did not reach completion marker; results so far:\n%.2000s", dump[i:])
	}
	failures := []string{}
	for _, line := range strings.Split(dump[i:], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL") {
			failures = append(failures, line)
		}
		if strings.HasPrefix(line, "SKIP") {
			t.Logf("unexecuted: %s", line)
		}
	}
	if len(failures) > 0 {
		t.Fatalf("dom assertions failed:\n%s", strings.Join(failures, "\n"))
	}
}

func startDomServer(t *testing.T, b Backend, ready chan struct{}, wraps ...func(http.Handler) http.Handler) (*Server, context.CancelFunc) {
	t.Helper()
	server, err := NewServer(b)
	if err != nil {
		t.Fatal(err)
	}
	if ready != nil {
		server.handler = domDriverMiddleware(t, ready)(server.handler)
	}
	// Wraps must be in place before Serve captures the handler; assigning
	// server.handler after startDomServer returns races the goroutine's read.
	for _, wrap := range wraps {
		server.handler = wrap(server.handler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	return server, cancel
}

// keepDomWindowAlive posts the page's own heartbeat for one window until the
// test ends, so a browser that is still cold-starting on a loaded runner cannot
// outlast heartbeatGrace before the page's first heartbeat. It adds no UI
// evidence: the runtime idle timeout still keys off real activity
// (activity=false), and every assertion still comes from the driver's
// RESULT/END block and the server state the page wrote.
func keepDomWindowAlive(t *testing.T, server *Server, windowID string) {
	t.Helper()
	host := server.Addr()
	body := fmt.Sprintf(`{"window":%q,"activity":false}`, windowID)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+"/api/window/heartbeat", strings.NewReader(body))
			if err == nil {
				request.Header.Set("Origin", "http://"+host)
				request.Header.Set("X-Formal-Gates-Token", server.token)
				if response, err := http.DefaultClient.Do(request); err == nil {
					response.Body.Close()
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func TestPanelDOMLibraryFlows(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 4)
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL)
	t.Logf("window result after run: %+v", window.Result())
	domResults(t, dump)
	// Server-side truth: the writes the DOM drove must have landed.
	state, err := Backend{Root: root, Home: home}.State()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, o := range state.Configurations {
		names[o.Name] = true
	}
	for _, want := range []string{"默认 副本", "工作", "新配置"} {
		if !names[want] {
			t.Fatalf("configuration %q missing after DOM flows; have %v", want, names)
		}
	}
	if state.Effective.Values.PanelIdleTimeout != 42 || state.Effective.Values.PanelSelection {
		t.Fatalf("panel settings not persisted: %+v", state.Effective.Values)
	}
}

func TestPanelDOMSupersededIntake(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	candidate := intakeFixture(t, root)
	ready := make(chan struct{}, 4)
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready)
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	// Flip the candidate stale once the intake page is live; the sweep must
	// supersede the waiter and the page's own window poll must surface it.
	go func() {
		select {
		case <-ready:
		case <-time.After(45 * time.Second):
		}
		if err := os.WriteFile(candidate, []byte("# changed intake\n"), 0o600); err != nil {
			t.Errorf("supersede fixture: %v", err)
		}
	}()
	dump := runDOM(t, browser, window.URL)
	domResults(t, dump)
	if r := window.Result(); r.Status != "superseded" {
		t.Fatalf("intake result = %+v", r)
	}
}

// saveLog records the runId of each configuration save the page issues, so a
// DOM test can prove the intake window is not attached to an existing run.
type saveLog struct {
	mu     sync.Mutex
	runIDs []string
}

func (l *saveLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.runIDs...)
}

func recordSaves(l *saveLog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && r.URL.Path == "/api/configurations" {
				body, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(body))
				var req struct {
					RunID string `json:"runId"`
				}
				if err := json.Unmarshal(body, &req); err == nil {
					l.mu.Lock()
					l.runIDs = append(l.runIDs, req.RunID)
					l.mu.Unlock()
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// An intake window opened beside an unfinished run must stay a pure
// configuration editor: the run's locks, plan and binding never leak into it.
func TestPanelDOMIntakeIgnoresExistingRun(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root, f, _ := panelRun(t, home)
	claimFirstMaterial(t, f)
	if _, err := settings.SaveConfiguration(root, home, "project", "新受理", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "新受理", nil); err != nil {
		t.Fatal(err)
	}
	candidate := intakeFixture(t, root)
	saved := &saveLog{}
	ready := make(chan struct{}, 4)
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready, recordSaves(saved))
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	// Once the page has saved and re-dirtied its draft, supersede the
	// candidate so the window must drop back to the real run context.
	go func() {
		select {
		case <-ready:
		case <-time.After(45 * time.Second):
		}
		if err := os.WriteFile(candidate, []byte("# changed intake\n"), 0o600); err != nil {
			t.Errorf("supersede fixture: %v", err)
		}
	}()
	dump := runDOM(t, browser, window.URL+"&probe=intakectx")
	domResults(t, dump)
	ids := saved.all()
	if len(ids) == 0 {
		t.Fatal("no save reached the backend")
	}
	for _, id := range ids {
		if id != "" {
			t.Fatalf("intake save carried run id %q", id)
		}
	}
}

// cdpDOM drives the panel through the test-only CDP probe so the page runs at
// an exact CSS device viewport --window-size cannot guarantee.
func cdpDOM(t *testing.T, browser, url string, width, height int, screenshots ...string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	args := append([]string{filepath.Join("testdata", "cdp_probe.js"), browser, url, fmt.Sprint(width), fmt.Sprint(height)}, screenshots...)
	out, err := exec.CommandContext(ctx, node, args...).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("cdp probe timed out:\n%.2000s", out)
	}
	if err != nil {
		t.Fatalf("cdp probe failed: %v\n%.2000s", err, out)
	}
	return string(out)
}

func TestPanelDOMMobileBounds(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := cdpDOM(t, browser, window.URL+"&probe=bounds&wantW=390&wantH=844", 390, 844)
	domResults(t, dump)
}

func TestPanelDOMDesktopScreenshot(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, nil)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	out := os.Getenv("PANEL_SHOT_DIR")
	if out == "" {
		out = t.TempDir()
	}
	shot := filepath.Join(out, fmt.Sprintf("desktop-%d.png", time.Now().Unix()))
	profile := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Same headless-browser constraint as runDOM.
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--no-sandbox", "--disable-gpu", "--user-data-dir="+profile,
		"--window-size=1440,900", "--virtual-time-budget=15000", "--screenshot="+shot, window.URL)
	if body, err := cmd.CombinedOutput(); err != nil {
		// The rendered file is the evidence: a browser that wrote it but could
		// not be reaped cleanly still produced the artifact under test.
		if info, statErr := os.Stat(shot); statErr != nil || info.Size() < 10000 {
			t.Fatalf("screenshot: %v\n%.1000s", err, body)
		}
		t.Logf("screenshot browser did not exit cleanly (%v), using the rendered file", err)
	}
	if info, err := os.Stat(shot); err != nil || info.Size() < 10000 {
		t.Fatalf("screenshot missing or tiny: %v %v", shot, err)
	}
	t.Logf("screenshot %s", shot)
}

func TestPanelDOMQAToggles(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=qa")
	domResults(t, dump)
}

func TestPanelDOMGateSelection(t *testing.T) {
	browser := panelBrowser(t)
	for _, directory := range []string{"available", "empty", "failed"} {
		t.Run(directory, func(t *testing.T) {
			home := panelHome(t)
			root := t.TempDir()
			configuration := settings.DefaultConfiguration()
			if directory != "available" {
				gates := []string{"unreported-gate"}
				configuration.Gates = &gates
			}
			if _, err := settings.WriteConfiguration(root, home, "project", "门配置", configuration, false, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := settings.UseConfiguration(root, home, "project", "门配置", nil); err != nil {
				t.Fatal(err)
			}
			gateDirectory := func(next http.Handler) http.Handler {
				mux := http.NewServeMux()
				mux.HandleFunc("GET /api/gates", func(w http.ResponseWriter, r *http.Request) {
					if directory == "failed" {
						http.Error(w, `{"error":"gate directory unavailable"}`, http.StatusServiceUnavailable)
						return
					}
					gates := []map[string]string{}
					if directory == "available" {
						gates = []map[string]string{
							{"id": "complexity-gate", "description": "检查职责与复杂度。\nCheck ownership and boundaries.\n" + strings.Repeat("UnbrokenGateDescription", 12) + "\n<em>保持文本</em>"},
							{"id": "implementation-quality-gate"},
						}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"gates": gates})
				})
				mux.Handle("/", next)
				return mux
			}
			server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4), execselMiddleware(&tupleLog{}), gateDirectory)
			window, err := server.NewWindow("")
			if err != nil {
				t.Fatal(err)
			}
			keepDomWindowAlive(t, server, window.ID)
			dump := cdpDOM(t, browser, window.URL+"&probe=gates&directory="+directory, 1440, 900)
			domResults(t, dump)
			if directory == "available" {
				out := os.Getenv("PANEL_SHOT_DIR")
				if out == "" {
					out = t.TempDir()
				}
				shot := filepath.Join(out, "gate-descriptions-mobile.png")
				domResults(t, cdpDOM(t, browser, window.URL+"&probe=gates&directory=available&layout=1", 390, 844, shot))
				t.Logf("gate description screenshot %s", shot)
			}
		})
	}
}

// TestPanelDOMTwoRuns switches between two runs bound to different
// configurations on the same root/admission fixture and checks the bound
// configuration loads rather than only the selector label changing.
func TestPanelDOMTwoRuns(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root, _, _ := panelRun(t, home)
	panelBoundRun(t, root, home, "panel-run-2", "备选")
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=runs")
	domResults(t, dump)
}

// TestPanelDOMRevisionConflict: a dirty draft, a legitimate external settings
// edit, then a panel flag edit and a config save must surface the conflict
// instead of silently absorbing the external write.
func TestPanelDOMRevisionConflict(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 4)
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	go func() {
		select {
		case <-ready:
		case <-time.After(60 * time.Second):
			return
		}
		b := Backend{Root: root, Home: home}
		st, err := b.State()
		if err != nil {
			t.Errorf("external edit state: %v", err)
			return
		}
		if _, err := b.EditPanelSetting("panelIdleTimeout", "77", st.Effective.Revisions); err != nil {
			t.Errorf("external edit: %v", err)
		}
	}()
	dump := runDOM(t, browser, window.URL+"&probe=revisions")
	domResults(t, dump)
}

// TestPanelDOMSaveCancel: cancelling save mode via Esc or the scrim must drop
// the pending continuation so a later unrelated save cannot run it.
func TestPanelDOMSaveCancel(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=savecancel")
	domResults(t, dump)
}

// tupleLog records the execution-options query tuples the page actually sent.
type tupleLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *tupleLog) add(v string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, v)
	count := 0
	for _, entry := range l.entries {
		if entry == v {
			count++
		}
	}
	return count
}
func (l *tupleLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// execselMiddleware serves a faithful execution-options fixture with distinct
// per-provider catalogues, records each requested tuple, and delays the codex
// response so a stale same-node reply can be observed losing to a newer one.
// Like the native adapters it fills effort options only for the queried
// model, so a model the page has never asked about carries no effort list.
func execselMiddleware(log *tupleLog) func(http.Handler) http.Handler {
	type entry struct {
		value   string
		efforts []string
		def     string
	}
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/execution-options", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			visit := log.add(q.Get("configuration") + "|" + q.Get("node") + "|" + q.Get("platform") + "|" + q.Get("model"))
			platform := q.Get("platform")
			platforms := []string{"codex", "zcode", "devin"}
			installed := map[string]any{"codex": true, "zcode": true, "devin": false}
			if platform == "default" {
				// A descriptor without an execution-selection adapter stays on
				// the unavailable host path, never a selectable platform.
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"node":        map[string]any{"id": q.Get("node")},
					"platform":    "default",
					"platforms":   platforms,
					"installed":   installed,
					"effective":   map[string]any{"provider": "codex"},
					"unavailable": true,
					"host":        true,
				})
				return
			}
			if platform == "devin" {
				// devin is adaptered but reports no model or effort catalogue,
				// and its executable is absent from PATH here.
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"node":      map[string]any{"id": q.Get("node")},
					"platform":  "devin",
					"platforms": platforms,
					"installed": installed,
					"effective": map[string]any{"provider": "codex"},
					"catalogue": map[string]any{"models": []any{}, "source": "execsel fixture"},
				})
				return
			}
			if platform == "codex" {
				time.Sleep(400 * time.Millisecond)
			}
			if platform == "codex" && q.Get("model") == "model-A" && visit == 2 {
				http.Error(w, `{"error":"native directory temporarily unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			entries := []entry{{"model-A", []string{"a-low", "a-high"}, "a-low"}}
			def := map[string]any{"model": "model-A", "effort": "a-high"}
			if platform == "zcode" {
				entries = []entry{
					{"model-B", []string{"b-low", "b-high"}, "b-low"},
					{"model-B2", []string{"b2-low", "b2-high"}, "b2-low"},
				}
				def = map[string]any{"model": "model-B", "effort": "b-high"}
			}
			if platform == "codex" && q.Get("model") == "model-A" && visit >= 3 {
				entries = append(entries, entry{"fast", []string{"a-low", "a-high"}, "a-low"})
				def = map[string]any{"model": "fast", "effort": "a-high"}
			}
			target := q.Get("model")
			if target == "" {
				target = def["model"].(string)
			}
			models := []map[string]any{}
			for _, e := range entries {
				m := map[string]any{"value": e.value, "label": e.value}
				if e.value == "fast" {
					m["label"] = "快速"
				}
				if e.value == target {
					m["efforts"] = e.efforts
					m["defaultEffort"] = e.def
				}
				models = append(models, m)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"node":      map[string]any{"id": q.Get("node")},
				"platform":  platform,
				"platforms": platforms,
				"installed": installed,
				"effective": map[string]any{"provider": "codex"},
				"catalogue": map[string]any{"models": models, "default": def, "source": "execsel fixture"},
			})
		})
		mux.Handle("/", next)
		return mux
	}
}

// TestPanelDOMDynamicSelect: a committed provider/model change must refresh
// the dependent controls while the inspector keeps focus, a stale slower
// same-node response must never append into the newer form, an unsaved draft
// still queries the catalogue through the builtin base, and a duplicate new
// name is refused without touching the draft or library.
func TestPanelDOMDynamicSelect(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "工作", nil); err != nil {
		t.Fatal(err)
	}
	// A saved provider whose executable is absent from PATH must stay visible
	// (marked 未安装), and a saved provider that names the orchestrating host
	// must render as a clean 宿主（主代理） state, not a dumped internal error.
	adapterless := settings.Configuration{Execution: map[string]settings.ExecutionChoice{"repair": {Provider: "devin"}}}
	if _, err := settings.WriteConfiguration(root, home, "project", "devin配置", adapterless, false, nil); err != nil {
		t.Fatal(err)
	}
	hostBound := settings.Configuration{Execution: map[string]settings.ExecutionChoice{"repair": {Provider: "default"}}}
	if _, err := settings.WriteConfiguration(root, home, "project", "宿主配置", hostBound, false, nil); err != nil {
		t.Fatal(err)
	}
	log := &tupleLog{}
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4), execselMiddleware(log))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	var dump string
	if os.Getenv("PANEL_DOM_REALTIME") != "" {
		dump = cdpDOM(t, browser, window.URL+"&probe=execsel", 1440, 900)
	} else {
		dump = runDOM(t, browser, window.URL+"&probe=execsel")
	}
	defer func() {
		if !t.Failed() {
			return
		}
		t.Logf("execution-option tuples queried: %v", log.all())
		if i := strings.Index(dump, `id="insp"`); i >= 0 {
			start := max(0, i-200)
			t.Logf("inspector region of dump: %.4000s", dump[start:])
		}
	}()
	domResults(t, dump)
	for _, entry := range log.all() {
		parts := strings.Split(entry, "|")
		if len(parts) != 4 {
			t.Fatalf("recorded tuple malformed: %q", entry)
		}
		if parts[0] != "默认" || parts[1] != "development" {
			t.Fatalf("catalogue query did not use the shared builtin context: %q", entry)
		}
	}
	// Nodes and configurations share provider/model answers, including the
	// same native catalogue reached through different saved overrides.
	want := []string{
		"默认|development||",
		"默认|development|zcode|",
		"默认|development|zcode|model-B2",
		"默认|development|zcode|model-B",
		"默认|development|codex|",
		"默认|development|devin|",
		"默认|development|default|",
		"默认|development|codex|model-A",
		"默认|development|codex|model-A",
		"默认|development|codex|model-A",
		"默认|development|codex|model-A",
		"默认|development|zcode|",
	}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("option queries = %v, want exactly %v", got, want)
	}
}

// injectStateMiddleware serves the real backend state plus the test's extra
// run summaries and binding entries so geometry and badge flows can run
// against a faithful projection without seeding run directories.
func injectStateMiddleware(backend Backend, extra []RunSummary, bindings map[string][]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
			state, err := backend.State()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			state.Runs = append(state.Runs, extra...)
			for name, ids := range bindings {
				state.Bindings[name] = append(state.Bindings[name], ids...)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(state)
		})
		mux.Handle("/", next)
		return mux
	}
}

// TestPanelDOMZoomFloorKeepsClickedNode: on a graph too wide to fit at the
// wheel minimum, fit must clamp at the minimum and keep the exact clicked
// repeated node instance inside the canvas rather than centering overflow.
func TestPanelDOMZoomFloorKeepsClickedNode(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	backend := Backend{Root: root, Home: home}
	injected := RunSummary{
		RunID: "zoom-run", Status: "RUNNING", Configuration: "默认",
		Flow: &protocol.FlowProgress{
			PlanConfirmed: true,
			DevelopmentBatches: []protocol.DevelopmentBatchProgress{
				{ID: "b1", Required: true, Status: runtime.TaskTerminal},
				{ID: "b2", Required: true, DependsOn: []string{"b1"}, Status: runtime.TaskRunning},
				{ID: "ab", Owner: "a", Required: false, DependsOn: []string{"za"}},
				{ID: "za", Owner: "z", Required: false},
			},
		},
		Slices: []SliceProgress{
			{ID: "a", RunID: "zoom-a", Status: "RUNNING", Flow: &protocol.FlowProgress{PlanConfirmed: true,
				DevelopmentBatches: []protocol.DevelopmentBatchProgress{{ID: "ab", Required: true, DependsOn: []string{"za"}, Status: runtime.TaskQueued}}}},
			{ID: "z", RunID: "zoom-z", Status: "RUNNING", Flow: &protocol.FlowProgress{PlanConfirmed: true,
				DevelopmentBatches: []protocol.DevelopmentBatchProgress{{ID: "za", Required: true, Status: runtime.TaskTerminal}}}},
		},
	}
	server, _ := startDomServer(t, backend, make(chan struct{}, 4), injectStateMiddleware(backend, []RunSummary{injected}, nil))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=zoomfit")
	domResults(t, dump)
}

// TestPanelDOMBoundRunsBadge: a configuration bound by more than two runs
// collapses the badge to a count that expands into a bounded list inside the
// card rather than spilling every run label across it.
func TestPanelDOMBoundRunsBadge(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "别的", "", nil); err != nil {
		t.Fatal(err)
	}
	backend := Backend{Root: root, Home: home}
	bindings := map[string][]string{"别的": {"ghost-1", "ghost-2", "ghost-3"}}
	server, _ := startDomServer(t, backend, make(chan struct{}, 4), injectStateMiddleware(backend, nil, bindings))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=boundruns")
	domResults(t, dump)
}

// TestPanelDOMBadConfigRecovery opens a window while the project overlay is
// invalid JSON, checks the real error shows, restores the exact valid bytes,
// and confirms the page loads the real configuration without reopening.
func TestPanelDOMBadConfigRecovery(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	if _, err := settings.SaveConfiguration(root, home, "project", "修复后", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "修复后", nil); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(root, ".gates", "config.json")
	valid, err := os.ReadFile(projectFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectFile, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 4)
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	go func() {
		select {
		case <-ready:
		case <-time.After(60 * time.Second):
			return
		}
		if err := os.WriteFile(projectFile, valid, 0o600); err != nil {
			t.Errorf("restore config: %v", err)
		}
	}()
	dump := runDOM(t, browser, window.URL+"&probe=badjson")
	domResults(t, dump)
}

// flowCanvasProbe reports through the same RESULT lines the driver uses. It runs
// inside the page as part of the served app.js (see domProbeMiddleware), so it
// reads the applied view directly instead of re-deriving it from styles. step
// names the phase a crash happened in, and each step guards one user-visible
// behaviour.
const flowCanvasProbe = `
(async () => {
const R = [];
let step = "init";
const fin0 = () => { let p = document.querySelector("#probeResult"); if (!p) { p = document.createElement("pre"); p.id = "probeResult"; document.body.appendChild(p); } p.textContent = "RESULT\n" + R.join("\n"); };
const ok = (n, c) => { R.push((c ? "PASS" : "FAIL") + " " + n); fin0(); };
const sleep = ms => new Promise(r => setTimeout(r, ms));
const pace = () => fetch("/api/state").catch(() => {});
const until = async (fn, n) => { for (let i = 0; i < (n || 240); i++) { const v = await fn(); if (v) return v; await pace(); await sleep(50); } return null; };
const q = s => document.querySelector(s);
const click = e => e && e.dispatchEvent(new MouseEvent("click", { bubbles: true }));
const viewOf = () => ({ x: view.x, y: view.y, z: view.z });
const same = (a, b) => a.x === b.x && a.y === b.y && a.z === b.z;
const labelOf = key => { const n = q('.node[data-key="' + key + '"]'); const t = n && n.querySelector(".t"); return t ? t.textContent : null; };

// The injected run renders once init() has loaded state; every step below
// inspects that render.
await until(() => document.querySelectorAll(".node").length >= 4);
// Step "role labels": a confirmed plan with slices names each repeated
// development box by its role (batch, slice batch, slice gate) and repeats the
// slice identity in the lane header, so a lane of batches cannot be mistaken for
// one more review stage.
step = "role labels";
ok("master batch label names its role", labelOf("n-dev-b1") === "开发批次 b1");
ok("slice batch label names its role", labelOf("sl-dev-core-sb1") === "开发分片 core · 批次 sb1");
ok("slice gate label names its role", labelOf("sl-gate-core") === "开发分片 core · 分片门");
ok("lane header names its role", [...document.querySelectorAll(".group")].some(g => g.textContent.indexOf("开发分片 core") === 0));

// Step "node header": the status pill sits in the title row and never overlaps
// the label, every pill carries text, an ended card shows no glyph while a
// closed card stays solid, responsibility rails keep distinct colours, and the
// legend matches what the canvas draws.
step = "node header";
const nodes = [...document.querySelectorAll(".node")];
ok("status pill sits in the title row", nodes.every(n => { const st = n.querySelector(".st"); return !st || st.parentElement.classList.contains("head"); }));
ok("status pills are never empty", nodes.every(n => [...n.querySelectorAll(".st")].every(st => st.textContent.trim() !== "")));
const overlap = nodes.filter(n => {
	const t = n.querySelector(".t"), st = n.querySelector(".st");
	if (!t || !st) return false;
	const a = t.getBoundingClientRect(), b = st.getBoundingClientRect();
	return a.width > 0 && b.width > 0 && a.right > b.left + 1 && a.left < b.right - 1 && a.bottom > b.top + 1 && a.top < b.bottom - 1;
}).length;
ok("labels never overlap their status pill", overlap === 0);
ok("finished nodes drop the lock glyph", !/🔒/.test(String(getComputedStyle(q(".node.done .t"), "::after").content)));
ok("closed nodes are solid cards", getComputedStyle(q(".node.disabled")).borderTopStyle === "solid");
const rail = resp => getComputedStyle(q('.node[data-resp="' + resp + '"]'), "::before").backgroundColor;
ok("responsibility rails carry distinct colours", rail("development") !== rail("gate") && rail("gate") === "rgb(246, 196, 83)" && rail("repair") !== rail("qa-blackbox"));
ok("legend lists the repair cycle", document.querySelectorAll(".legend span").length === 5 && getComputedStyle(q(".legend .sw.loop")).borderTopStyle === "dashed");
ok("legend swatch matches the ended node look", getComputedStyle(q(".legend .sw.done")).backgroundColor === getComputedStyle(q(".node.done")).backgroundColor);

// Step "seal node": the seal renders as a non-configurable terminal operation —
// not focusable, not selectable, and it opens no inspector.
step = "seal node";
const seal = q('.node[data-key="v-seal"]');
ok("terminal seal node rendered", !!seal);
ok("seal node is a non-configurable operation", !!seal && seal.classList.contains("op") && seal.classList.contains("terminal") && seal.getAttribute("tabindex") === "-1");
click(seal);
await sleep(120);
ok("seal node cannot be selected", !q(".node.sel") && !q(".inspector.open"));

// Step "repair return manifold": one return path per verification head, and no
// sampled point of any path falls inside a node box; the manifold is captioned.
step = "repair return manifold";
const loops = [...document.querySelectorAll("#edges path.loop")];
const loopEdges = graph.edges.filter(e => e.cls === "loop").length;
ok("repair loop is drawn as one return path per verification node (" + loops.length + "/" + loopEdges + ")", loopEdges === 3 && loops.length === loopEdges);
const boxes = () => [...document.querySelectorAll(".node")].map(n => ({ x: parseFloat(n.style.left), y: parseFloat(n.style.top), w: n.offsetWidth, h: n.offsetHeight }));
let inside = 0, samples = 0;
for (const p of loops) {
	const len = p.getTotalLength();
	for (let d = 0; d <= len; d += 2) {
		const pt = p.getPointAtLength(d);
		samples++;
		for (const b of boxes()) if (pt.x > b.x + 1 && pt.x < b.x + b.w - 1 && pt.y > b.y + 1 && pt.y < b.y + b.h - 1) inside++;
	}
}
ok("repair loop never enters a node box (" + inside + " of " + samples + " samples inside)", samples > 0 && inside === 0);
ok("manifold is labelled", [...document.querySelectorAll("#edges text.loop-label")].some(t => t.textContent === "返修后重跑验证"));

// Step "edge layers": every link is a dashed animated line (only the hot layers
// drop the animation under reduced motion), the seal link is teal, a closed node
// dims its links, a running node brightens them, and a selected node colours its
// own links.
step = "edge layers";
const edges = [...document.querySelectorAll("#edges path.edge")];
const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
ok("every connection is a dashed line (animated unless reduced motion: reduce=" + reduce + ")", edges.length > 0 && edges.every(p => {
	const anim = getComputedStyle(p).animationName, hot = p.classList.contains("hot-run") || p.classList.contains("hot-sel");
	return parseFloat(getComputedStyle(p).strokeDasharray) > 0 && anim === (reduce && hot ? "none" : "dash");
}));
ok("seal link is drawn teal", !!q("#edges path.seal-edge"));
ok("closed nodes dim their links", document.querySelectorAll("#edges path.off").length > 0);
ok("running nodes brighten their links", document.querySelectorAll("#edges path.hot-run").length > 0);
click(q('.node[data-key="v-gate"]'));
await until(() => q(".inspector.open"));
ok("selected node colours its links", document.querySelectorAll("#edges path.hot-sel").length > 0);

// Step "pan boundary and view retention": a drag stops with the content one
// VIEW_PAD inside the visible band (drawer inset to inspector inset), vertically
// fitting content stays on canvas, closing the inspector keeps the view, an
// untouched drawer refits on close, an adjusted view is only clamped by the
// freed width, and the scrim close keeps it as well.
step = "pan boundary and view retention";
const canvasEl = q("#canvas");
const down = (x, y) => canvasEl.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, clientX: x, clientY: y }));
const up = () => window.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
const drag = (x0, y0, x1, y1) => { down(x0, y0); window.dispatchEvent(new MouseEvent("mousemove", { bubbles: true, clientX: x1, clientY: y1 })); up(); };
const settleClick = () => { down(320, 320); up(); click(canvasEl); };
// The visible canvas band starts at the drawer inset and ends where the
// inspector starts; the pan boundary keeps the content covering that band, so
// a drag can leave at most VIEW_PAD of gap at a band edge.
const band = () => { const L = drawerInset(); return { L, W: canvasEl.clientWidth - inspectorInset() - L }; };
const contentLeft = () => view.x + contentBox().x0 * view.z;
const contentRight = () => view.x + contentBox().x1 * view.z;
drag(300, 300, 3300, 2300);
const panned = viewOf();
{
	const b = band(), left = contentLeft(), right = contentRight();
	ok("a large drag stops at the pan boundary (content " + Math.round(left) + ".." + Math.round(right) + ", band " + Math.round(b.L) + ".." + Math.round(b.L + b.W) + ", pad " + VIEW_PAD + ")",
		Math.abs(left - (b.L + VIEW_PAD)) <= 1.5 && right >= b.L + b.W);
	const box = contentBox(), H = canvasEl.clientHeight, top = view.y + box.y0 * view.z, bottom = view.y + box.y1 * view.z;
	ok("content that fits vertically stays inside the canvas after a drag", top >= 0 && bottom <= H + 1);
}
drag(300, 300, 3300, 2300);
ok("dragging further cannot cross the boundary", same(viewOf(), panned));
settleClick();
await until(() => !q(".inspector.open"));
ok("blank canvas click closes the inspector and keeps the view", !q(".inspector.open") && same(viewOf(), panned));
click(q('.node[data-key="v-gate"]'));
await until(() => q(".inspector.open"));
const beforeClose = viewOf();
click(q("#insp .iconbtn"));
await until(() => !q(".inspector.open"));
ok("inspector close button keeps the view", same(viewOf(), beforeClose));
// The band's right edge stops at the inspector, so a drag the other way parks
// the content's right edge one pad short of the open inspector.
click(q('.node[data-key="v-gate"]'));
await until(() => q(".inspector.open"));
drag(300, 300, -300, 300);
{
	const b = band(), right = contentRight();
	ok("a drag stops at the inspector boundary (content right " + Math.round(right) + ", band " + Math.round(b.L) + ".." + Math.round(b.L + b.W) + ", inset " + inspectorInset() + ")",
		inspectorInset() > 0 && Math.abs(right - (b.L + b.W - VIEW_PAD)) <= 1.5);
}
click(q("#insp .iconbtn"));
await until(() => !q(".inspector.open"));
// Open the drawer from either state: the open fits the graph beside it (left
// edge at the drawer inset plus the margin). Closing that untouched view must
// re-fit the canvas over the freed width, i.e. to the closed-drawer fit
// position with drawer inset L=0 and margin m=30.
if (q("#drawer").classList.contains("open")) { click(q("#libBtn")); await until(() => !q("#drawer").classList.contains("open")); }
click(q("#libBtn"));
await until(() => q("#drawer").classList.contains("open"));
const drawerView = viewOf();
click(q("#libBtn"));
await until(() => !q("#drawer").classList.contains("open"));
let refitLeft = 0;
{
	const m = 30, W = canvasEl.clientWidth, box = contentBox(), cw = (box.x1 - box.x0) * view.z;
	const wantLeft = cw > W - 2 * m ? m : (W - cw) / 2;
	const drawerLeft = drawerView.x + box.x0 * drawerView.z;
	refitLeft = contentLeft();
	ok("closing an untouched drawer refits the canvas (left " + Math.round(drawerLeft) + " -> " + Math.round(refitLeft) + ", want " + Math.round(wantLeft) + ")",
		Math.abs(refitLeft - wantLeft) <= 1.5 && refitLeft < drawerLeft);
}
// Closing after a pan keeps the adjusted view: the boundary moves by exactly
// the freed drawer width, so the view lands on the closed-drawer boundary
// instead of the closed-drawer fit position.
click(q("#libBtn"));
await until(() => q("#drawer").classList.contains("open"));
const drawerW = q("#drawer").offsetWidth;
drag(600, 420, 660, 460);
const adjustedDrawerView = viewOf();
const openBoundLeft = contentLeft();
click(q("#libBtn"));
await until(() => !q("#drawer").classList.contains("open"));
{
	const left = contentLeft();
	ok("library toggle keeps an adjusted view clamped to the closed boundary (left " + Math.round(left) + ", boundary " + Math.round(openBoundLeft - drawerW) + ", refit " + Math.round(refitLeft) + ", view.y " + Math.round(view.y) + ")",
		Math.abs(left - (openBoundLeft - drawerW)) <= 1.5 && Math.abs(left - refitLeft) > 1.5 &&
		view.z === adjustedDrawerView.z && Math.abs(view.y - adjustedDrawerView.y) <= 0.5);
}
click(q("#libBtn"));
await until(() => q("#drawer").classList.contains("open"));
drag(600, 420, 200, 420);
const scrimView = viewOf();
click(q("#scrim"));
await until(() => !q("#drawer").classList.contains("open"));
ok("scrim close keeps an adjusted view", same(viewOf(), scrimView) && Math.abs(contentLeft() - refitLeft) > 1.5);

// Step "zoom control": the bar carries four buttons on the canvas, its
// percentage mirrors the applied scale through zoom-in and the overview, and it
// steps aside while the inspector is open.
step = "zoom control";
ok("desktop viewport for the canvas layout assertions (w=" + window.innerWidth + ")", window.innerWidth > 760);
const bar = q(".zoombar");
const pct = bar && bar.querySelector(".pct");
ok("zoom control sits on the canvas", !!bar && bar.querySelectorAll("button").length === 4);
ok("zoom percentage reflects the applied scale", !!pct && pct.textContent === Math.round(view.z * 100) + "%");
const zIn = view.z;
click(bar.querySelector('[data-z="in"]'));
ok("zoom in raises the scale", view.z > zIn && pct.textContent === Math.round(view.z * 100) + "%");
click(bar.querySelector('[data-z="fit"]'));
ok("overview frames the whole graph", view.z <= 1 && pct.textContent === Math.round(view.z * 100) + "%");
click(q('.node[data-key="v-gate"]'));
await until(() => q(".inspector.open"));
const gap = canvasEl.getBoundingClientRect().right - bar.getBoundingClientRect().right;
ok("zoom control steps aside for the open inspector (gap " + Math.round(gap) + ")", gap > 300);
click(q("#insp .iconbtn"));
await until(() => !q(".inspector.open"));

// Step "render guard": a poll that changes nothing keeps the same node and edge
// elements, so the flow animation is not restarted, while a selection change
// rebuilds them.
step = "render guard";
const kept = q('.node[data-key="v-qa-b"]');
kept.dataset.probeMark = "kept";
const keptEdge = q("#edges path");
await sleep(4600);
ok("graph DOM survives state polls", q('.node[data-key="v-qa-b"]') === kept && kept.dataset.probeMark === "kept" && q("#edges path") === keptEdge);
const gateBefore = q('.node[data-key="v-gate"]');
click(gateBefore);
await until(() => q(".inspector.open"));
ok("a selection change rebuilds the graph DOM", q('.node[data-key="v-gate"]') !== gateBefore);

// Step "configuration rebuild": switching a responsibility off rebuilds the
// graph, the node reads as a closed card and flashes as changed.
step = "configuration rebuild";
const qaBefore = q('.node[data-key="v-qa-b"]');
click(qaBefore);
await until(() => q(".inspector .tg"));
click(q(".inspector .tg"));
await until(() => q('.node[data-key="v-qa-b"]') !== qaBefore);
ok("a configuration change rebuilds the graph DOM and its node state",
	q('.node[data-key="v-qa-b"]') !== qaBefore && q('.node[data-key="v-qa-b"]').classList.contains("disabled"));
ok("a changed node keeps its flash hint", q('.node[data-key="v-qa-b"]').classList.contains("flash"));
R.push("END");
fin0();
})().catch(e => {
	const p = document.createElement("pre");
	p.id = "probeResult";
	p.textContent = "RESULT\nFAIL probe-crashed step=" + step + " err=" + (e && e.message) + " @" + (e && e.stack && e.stack.split("\n")[1]);
	document.body.appendChild(p);
});
`

// domProbeMiddleware appends one behaviour probe to the served app.js, so the
// page runs it as part of the real renderer: the probe shares app.js's script
// scope and reads the renderer's own top-level bindings (view, graph,
// contentBox, drawerInset, inspectorInset, VIEW_PAD) instead of guessing the
// applied state from computed styles. No production code path learns about
// tests.
func domProbeMiddleware(t *testing.T, probe string) func(http.Handler) http.Handler {
	t.Helper()
	app, err := ui.ReadFile("ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Write(app)
			w.Write([]byte(probe))
		})
		mux.Handle("/", next)
		return mux
	}
}

// TestPanelDOMFlowCanvasRendering drives the rendered canvas over a confirmed
// plan with slices: role-explicit labels, the terminal seal node, the repair
// return manifold geometry, the pan boundary and view retention when a side
// panel closes, the zoom control, and the render guard that keeps the graph DOM
// across polls.
func TestPanelDOMFlowCanvasRendering(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root := t.TempDir()
	// Both QA modes on gives the three verification heads the return manifold
	// has to fan out to; a closed review node keeps a dimmed link on the canvas.
	configuration := settings.Configuration{
		QAModes:       []string{"blackbox", "whitebox"},
		FlowSelection: &settings.FlowSelection{Disabled: []string{"start-readiness"}},
	}
	if _, err := settings.WriteConfiguration(root, home, "project", "视觉", configuration, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "视觉", nil); err != nil {
		t.Fatal(err)
	}
	backend := Backend{Root: root, Home: home}
	injected := RunSummary{
		RunID: "flow-run", Status: "RUNNING", Configuration: "视觉",
		Nodes: []protocol.ExecutionNodeState{
			{Node: "development", Enabled: true, Running: true, Editable: true},
			{Node: "product-review", Enabled: true, Completed: true},
		},
		Flow: &protocol.FlowProgress{PlanConfirmed: true, DevelopmentBatches: []protocol.DevelopmentBatchProgress{
			{ID: "b1", Required: true, Status: runtime.TaskTerminal},
			{ID: "b2", Required: true, DependsOn: []string{"b1"}, Status: runtime.TaskRunning},
		}},
		Slices: []SliceProgress{
			{ID: "core", RunID: "flow-core", Status: "RUNNING", Flow: &protocol.FlowProgress{PlanConfirmed: true,
				DevelopmentBatches: []protocol.DevelopmentBatchProgress{{ID: "sb1", Required: true, Status: runtime.TaskRunning}}}},
		},
	}
	server, _ := startDomServer(t, backend, nil,
		injectStateMiddleware(backend, []RunSummary{injected}, nil),
		domProbeMiddleware(t, flowCanvasProbe))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	keepDomWindowAlive(t, server, window.ID)
	dump := runDOM(t, browser, window.URL+"&probe=flowcanvas", "--window-size=1440,900")
	domResults(t, dump)
}
