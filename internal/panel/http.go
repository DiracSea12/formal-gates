package panel

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
	"formal-gates/internal/settings"
)

//go:embed ui/index.html ui/app.js ui/style.css
var ui embed.FS

// configurationRequest is the typed body for POST /api/configurations.
type configurationRequest struct {
	Operation string                  `json:"operation"`
	Scope     string                  `json:"scope"`
	Name      string                  `json:"name"`
	NewName   string                  `json:"newName"`
	Source    string                  `json:"source"`
	Overwrite bool                    `json:"overwrite"`
	Candidate *settings.Configuration `json:"candidate"`
	RunID     string                  `json:"runId"`
	Expected  map[string]string       `json:"expected"`
}

// settingRequest is the typed body for POST /api/settings.
type settingRequest struct {
	Name     string            `json:"name"`
	Value    string            `json:"value"`
	Expected map[string]string `json:"expected"`
}

// intakeCheckRequest is the typed body for POST /api/intake/check.
type intakeCheckRequest struct {
	Candidate     string `json:"candidate"`
	Configuration string `json:"configuration"`
}

type handler struct {
	backend Backend
	host    string
	token   string
	server  *Server
}

// NewHandler serves the panel API. Every request must carry the loopback Host
// this listener bound; writes additionally require the page Origin and the
// per-launch token so another site cannot drive the endpoints.
func NewHandler(backend Backend, host, token string) http.Handler {
	h := &handler{backend: backend, host: host, token: token}
	mux := http.NewServeMux()
	h.registerRoutes(mux)
	return mux
}

func (h *handler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", h.state)
	mux.HandleFunc("GET /api/gates", h.gates)
	mux.HandleFunc("GET /api/execution-options", h.executionOptions)
	mux.HandleFunc("POST /api/configurations", h.configurations)
	mux.HandleFunc("POST /api/settings", h.settingsEdit)
	mux.HandleFunc("POST /api/intake/check", h.intakeCheck)
}

func (h *handler) guardRead(w http.ResponseWriter, r *http.Request) bool {
	if r.Host != h.host {
		writeError(w, http.StatusForbidden, "request rejected")
		return false
	}
	return true
}

func (h *handler) guardWrite(w http.ResponseWriter, r *http.Request) bool {
	if !h.guardRead(w, r) {
		return false
	}
	if r.Header.Get("Origin") != "http://"+h.host || r.Header.Get("X-Formal-Gates-Token") != h.token {
		writeError(w, http.StatusForbidden, "request rejected")
		return false
	}
	return true
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if err := harness.StrictDecode(body, value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// fail maps owner errors to readable responses. Revision conflicts report 409
// after zero writes; an absent registered runtime reports 503 on the options
// endpoints instead of failing the library state.
func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case strings.Contains(err.Error(), "changed since it was read"):
		status = http.StatusConflict
	case strings.Contains(err.Error(), "UNREGISTERED_INSTALL"):
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, err.Error())
}

func revisionsPresent(expected map[string]string) bool {
	_, user := expected["user"]
	_, project := expected["project"]
	return user && project
}

func (h *handler) state(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	state, err := h.backend.State()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, state)
}

func (h *handler) gates(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	gates, err := h.backend.Gates()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"gates": gates})
}

func (h *handler) executionOptions(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	query := r.URL.Query()
	options, err := h.backend.ExecutionOptions(query.Get("configuration"), facade.ExecutionSelectionInput{
		Node:     query.Get("node"),
		Platform: query.Get("platform"),
		Model:    query.Get("model"),
		Effort:   query.Get("effort"),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, options)
}

func (h *handler) configurations(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request configurationRequest
	if !decode(w, r, &request) {
		return
	}
	if request.Operation == "" {
		writeError(w, http.StatusBadRequest, "operation is required")
		return
	}
	if !revisionsPresent(request.Expected) {
		writeError(w, http.StatusBadRequest, "expected must carry both user and project revisions")
		return
	}
	var err error
	created := ""
	switch request.Operation {
	case "save":
		if request.Candidate == nil {
			writeError(w, http.StatusBadRequest, "candidate is required for save")
			return
		}
		_, err = h.backend.Save(request.Scope, request.Name, *request.Candidate, request.Overwrite, request.Expected, request.RunID)
	case "copy":
		created, err = h.backend.Copy(request.Scope, request.Source, request.Expected)
	case "rename":
		_, err = h.backend.Rename(request.Scope, request.Name, request.NewName, request.Expected)
	case "delete":
		_, err = h.backend.Delete(request.Scope, request.Name, request.Expected)
	case "use":
		_, err = h.backend.Use(request.Name, request.Expected)
	default:
		writeError(w, http.StatusBadRequest, "unknown operation "+request.Operation)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "name": created})
}

func (h *handler) settingsEdit(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request settingRequest
	if !decode(w, r, &request) {
		return
	}
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !revisionsPresent(request.Expected) {
		writeError(w, http.StatusBadRequest, "expected must carry both user and project revisions")
		return
	}
	effective, err := h.backend.EditPanelSetting(request.Name, request.Value, request.Expected)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "revisions": effective.Revisions})
}

func (h *handler) intakeCheck(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request intakeCheckRequest
	if !decode(w, r, &request) {
		return
	}
	if request.Candidate == "" || request.Configuration == "" {
		writeError(w, http.StatusBadRequest, "candidate and configuration are required")
		return
	}
	check, err := h.backend.CheckIntake(request.Candidate, request.Configuration)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, check)
}

// windowStatusRequest and windowActivityRequest are the typed bodies and query
// for the window session endpoints.
type windowSelectRequest struct {
	Intake        string `json:"intake,omitempty"`
	Window        string `json:"window"`
	Configuration string `json:"configuration"`
}

type windowHeartbeatRequest struct {
	Window   string `json:"window"`
	Activity bool   `json:"activity"`
}

type windowCloseRequest struct {
	Window string `json:"window"`
}

// registerWindowRoutes serves the embedded page, its assets, and the window
// session endpoints. The window id and service token reach the page through
// the escaped JSON bootstrap only; these endpoints expose no workflow control.
func (h *handler) registerWindowRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/instance", h.instanceIdentity)
	mux.HandleFunc("GET /api/window", h.windowGet)
	mux.HandleFunc("POST /api/windows/open", h.windowOpen)
	mux.HandleFunc("POST /api/window/heartbeat", h.windowHeartbeat)
	mux.HandleFunc("POST /api/window/select", h.windowSelect)
	mux.HandleFunc("POST /api/window/close", h.windowClose)
}

// windowOpenRequest opens or reuses an editor window for a lifecycle client.
// It manages windows only; nothing here starts, resets, or drives a run.
type windowOpenRequest struct {
	Intake string `json:"intake"`
}

func (h *handler) windowOpen(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request windowOpenRequest
	if !decode(w, r, &request) {
		return
	}
	window, reused, err := h.server.openWindow(request.Intake)
	if err != nil {
		fail(w, err)
		return
	}
	// The service owns the managed browser: a fresh window launches one on
	// its own profile and attaches it to every window end; a reused window
	// keeps the browser it already has.
	if !reused {
		if err := h.server.launchBrowser(window); err != nil {
			h.server.closeWindow(window.session, Result{Status: "closed"})
			writeError(w, http.StatusServiceUnavailable, openWindowErrorText(err))
			return
		}
	}
	writeJSON(w, map[string]any{"window": window.ID, "url": window.URL, "reused": reused})
}

// openWindowErrorText appends the non-panel settings path when the launch
// failed because no managed browser exists; any other failure keeps its own
// message.
func openWindowErrorText(err error) string {
	var unavailable *ErrBrowserUnavailable
	if !errors.As(err, &unavailable) {
		return err.Error()
	}
	return err.Error() + "; without the panel, per-node platform/model/effort can be set via: formal-gates settings set --execution-node <node> --platform <platform> --model <model> --effort <effort> (model and effort apply only when the platform reports them)"
}

// instance reports the serving process identity to a lifecycle client that
// already holds the record token; it answers regardless of configuration
// validity so a broken project config does not look like a dead service.
func (h *handler) instanceIdentity(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	if r.Header.Get("X-Formal-Gates-Token") != h.token {
		writeError(w, http.StatusForbidden, "request rejected")
		return
	}
	writeJSON(w, map[string]any{"pid": os.Getpid()})
}

func (h *handler) windowGet(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	h.server.mu.Lock()
	session, known := h.server.windows[r.URL.Query().Get("window")]
	if known && r.URL.Query().Get("wait") == "true" && session.result == nil {
		h.server.mu.Unlock()
		select {
		case <-session.done:
		case <-r.Context().Done():
			return
		}
		h.server.mu.Lock()
	}
	view := map[string]any{}
	if known {
		view = map[string]any{
			"window":     session.id,
			"mode":       session.mode,
			"intakePath": session.intakePath,
			"open":       session.open,
			"result":     session.result,
		}
	}
	h.server.mu.Unlock()
	if !known {
		writeError(w, http.StatusNotFound, "unknown window")
		return
	}
	writeJSON(w, view)
}

func (h *handler) windowHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request windowHeartbeatRequest
	if !decode(w, r, &request) {
		return
	}
	h.server.mu.Lock()
	defer h.server.mu.Unlock()
	session, ok := h.server.windows[request.Window]
	if !ok || !session.open {
		writeError(w, http.StatusNotFound, "unknown window")
		return
	}
	session.seen = h.server.now()
	if request.Activity {
		session.activity = session.seen
	}
	writeJSON(w, map[string]any{"ok": true})
}

// windowSelect runs the saved configuration through the intake checks. A
// superseded candidate resolves the caller's waiter but leaves the window
// open as a library/run window.
func (h *handler) windowSelect(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request windowSelectRequest
	if !decode(w, r, &request) {
		return
	}
	h.server.mu.Lock()
	session, known := h.server.windows[request.Window]
	if request.Intake != "" {
		known = false
		for _, current := range h.server.windows {
			if current.open && current.mode == "intake" && current.intakePath == request.Intake {
				session, known = current, true
				break
			}
		}
		if !known {
			h.server.mu.Unlock()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
	}
	open := known && session.open && session.mode == "intake"
	if known && session.open && session.mode == "library" && session.result != nil {
		result := *session.result
		h.server.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "result": result})
		return
	}
	h.server.mu.Unlock()
	if !open {
		writeError(w, http.StatusBadRequest, "window is not an open intake window")
		return
	}
	if request.Configuration == "" {
		writeError(w, http.StatusBadRequest, "configuration is required")
		return
	}
	supersede := func(reason string) {
		result := Result{Status: "superseded", Reason: reason}
		h.server.mu.Lock()
		if session.open && session.result == nil {
			h.server.resolveLocked(session, result)
			session.mode = "library"
		}
		if session.result != nil {
			result = *session.result
		}
		h.server.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "result": result})
	}
	fresh, err := h.server.backend.CheckIntakeFreshness(session.intakePath)
	if err != nil {
		fail(w, err)
		return
	}
	if fresh.Superseded {
		supersede(fresh.Reason)
		return
	}
	check, err := h.server.backend.CheckIntake(session.intakePath, request.Configuration)
	if err != nil {
		fail(w, err)
		return
	}
	if check.Superseded {
		supersede(check.Reason)
		return
	}
	result := Result{Status: "selected", Configuration: check.Selection.Configuration, Disabled: check.Selection.Disabled}
	h.server.closeWindow(session, result)
	writeJSON(w, map[string]any{"ok": true, "result": result})
}

func (h *handler) windowClose(w http.ResponseWriter, r *http.Request) {
	if !h.guardWrite(w, r) {
		return
	}
	var request windowCloseRequest
	if !decode(w, r, &request) {
		return
	}
	session, ok := h.server.session(request.Window)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown window")
		return
	}
	h.server.closeWindow(session, h.server.endResult(session, "cancelled"))
	writeJSON(w, map[string]any{"ok": true})
}

// registerAssetRoutes serves the embedded page and its static assets behind
// the same Host guard as the API; the page's bootstrap JSON carries the
// per-window identity and service token for the page's own write calls.
func (h *handler) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.page)
	mux.HandleFunc("GET /app.js", h.asset("ui/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /style.css", h.asset("ui/style.css", "text/css; charset=utf-8"))
}

func (h *handler) asset(path, contentType string) http.HandlerFunc {
	data, err := ui.ReadFile(path)
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.guardRead(w, r) {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}

// page injects the window identity and service token into the page as escaped
// JSON; encoding/json escapes <, >, & so the blob cannot break out of the
// script tag.
func (h *handler) page(w http.ResponseWriter, r *http.Request) {
	if !h.guardRead(w, r) {
		return
	}
	h.server.mu.Lock()
	session, known := h.server.windows[r.URL.Query().Get("window")]
	var bootstrap map[string]string
	if known {
		bootstrap = map[string]string{
			"window":     session.id,
			"mode":       session.mode,
			"intakePath": session.intakePath,
			"token":      h.token,
		}
	}
	h.server.mu.Unlock()
	if !known {
		writeError(w, http.StatusNotFound, "unknown window")
		return
	}
	bootJSON, err := json.Marshal(bootstrap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	page, err := ui.ReadFile("ui/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(bytes.Replace(page, []byte("__PANEL_BOOTSTRAP__"), bootJSON, 1))
}
