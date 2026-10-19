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
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

// panelBrowser locates an installed Chromium-family browser for headless DOM
// driving; nothing is downloaded.
func panelBrowser(t *testing.T) string {
	t.Helper()
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
	cmdArgs := append([]string{"--headless=new", "--disable-gpu", "--no-first-run", "--disable-extensions",
		"--user-data-dir=" + profile, "--virtual-time-budget=240000", "--dump-dom"}, args...)
	cmdArgs = append(cmdArgs, url)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, browser, cmdArgs...).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("browser timed out; partial dump is not evidence:\n%.2000s", out)
	}
	if err != nil {
		t.Fatalf("browser run failed: %v\n%.2000s", err, out)
	}
	return string(out)
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

func startDomServer(t *testing.T, b Backend, ready chan struct{}) (*Server, context.CancelFunc) {
	t.Helper()
	server, err := NewServer(b)
	if err != nil {
		t.Fatal(err)
	}
	if ready != nil {
		server.handler = domDriverMiddleware(t, ready)(server.handler)
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
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, ready)
	server.handler = recordSaves(saved)(server.handler)
	window, err := server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
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
func cdpDOM(t *testing.T, browser, url string, width, height int) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, filepath.Join("testdata", "cdp_probe.js"), browser, url,
		fmt.Sprint(width), fmt.Sprint(height)).CombinedOutput()
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
	out := os.Getenv("PANEL_SHOT_DIR")
	if out == "" {
		out = t.TempDir()
	}
	shot := filepath.Join(out, fmt.Sprintf("desktop-%d.png", time.Now().Unix()))
	profile := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--user-data-dir="+profile,
		"--window-size=1440,900", "--virtual-time-budget=15000", "--screenshot="+shot, window.URL)
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("screenshot: %v\n%.1000s", err, body)
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
	dump := runDOM(t, browser, window.URL+"&probe=qa")
	domResults(t, dump)
}

// TestPanelDOMTwoRuns switches between two runs bound to different
// configurations on the same root/admission fixture and checks the bound
// configuration loads rather than only the selector label changing.
func TestPanelDOMTwoRuns(t *testing.T) {
	browser := panelBrowser(t)
	home := panelHome(t)
	root, _, _ := panelRun(t, home)
	panelBoundRun(t, root, home, "panel-run-2", "轻量")
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
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
	dump := runDOM(t, browser, window.URL+"&probe=savecancel")
	domResults(t, dump)
}

// tupleLog records the execution-options query tuples the page actually sent.
type tupleLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *tupleLog) add(v string) { l.mu.Lock(); l.entries = append(l.entries, v); l.mu.Unlock() }
func (l *tupleLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// execselMiddleware serves a faithful execution-options fixture with distinct
// per-provider catalogues, records each requested tuple, and delays the codex
// response so a stale same-node reply can be observed losing to a newer one.
func execselMiddleware(log *tupleLog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/execution-options", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			log.add(q.Get("configuration") + "|" + q.Get("node") + "|" + q.Get("platform") + "|" + q.Get("model"))
			platform := q.Get("platform")
			if platform == "codex" {
				time.Sleep(400 * time.Millisecond)
			}
			models := []map[string]any{{"value": "model-A", "label": "model-A", "efforts": []string{"a-low", "a-high"}, "defaultEffort": "a-low"}}
			def := map[string]any{"model": "model-A", "effort": "a-low"}
			if platform == "zcode" {
				models = []map[string]any{
					{"value": "model-B", "label": "model-B", "efforts": []string{"b-low", "b-high"}, "defaultEffort": "b-low"},
					{"value": "model-B2", "label": "model-B2", "efforts": []string{"b2-low", "b2-high"}, "defaultEffort": "b2-low"},
				}
				def = map[string]any{"model": "model-B", "effort": "b-low"}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"node":      map[string]any{"id": q.Get("node")},
				"platform":  platform,
				"platforms": []string{"codex", "zcode"},
				"effective": map[string]any{"provider": platform, "model": q.Get("model")},
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
	log := &tupleLog{}
	server, _ := startDomServer(t, Backend{Root: root, Home: home}, make(chan struct{}, 4))
	server.handler = execselMiddleware(log)(server.handler)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
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
	found := false
	for _, entry := range log.all() {
		parts := strings.Split(entry, "|")
		if len(parts) != 4 {
			t.Fatalf("recorded tuple malformed: %q", entry)
		}
		if parts[0] != "默认" {
			t.Fatalf("unsaved draft did not borrow the builtin base: %q", entry)
		}
		if parts[1] == "repair" && parts[2] == "zcode" && parts[3] == "model-B2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("model-specific tuple never queried: %v", log.all())
	}
}

// injectStateMiddleware serves the real backend state plus the test's extra
// run summaries so geometry flows can run against a faithful multi-slice
// projection without seeding run directories.
func injectStateMiddleware(backend Backend, extra []RunSummary) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
			state, err := backend.State()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			state.Runs = append(state.Runs, extra...)
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
		RunID: "zoom-run", Status: "RUNNING", Route: "regular", Configuration: "默认",
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
	server, _ := startDomServer(t, backend, make(chan struct{}, 4))
	server.handler = injectStateMiddleware(backend, []RunSummary{injected})(server.handler)
	window, err := server.NewWindow("")
	if err != nil {
		t.Fatal(err)
	}
	dump := runDOM(t, browser, window.URL+"&probe=zoomfit")
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
