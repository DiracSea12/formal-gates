package panel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
	"formal-gates/internal/settings"
)

// Result is the resolved answer for one window's caller, stored in memory and
// readable by every waiter; the window itself may stay open afterwards.
type Result struct {
	Status        string   `json:"status"` // selected | timeout | cancelled | closed | superseded
	Configuration string   `json:"configuration,omitempty"`
	Disabled      []string `json:"disabled,omitempty"`
	Route         string   `json:"route,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// Window describes one browser session. Done closes when the caller's result
// resolves; Result reports it for any number of waiters.
type Window struct {
	ID      string
	URL     string
	Done    <-chan struct{}
	session *windowSession
}

// Result returns the resolved answer, or a zero Result while the window is
// still pending.
func (w Window) Result() Result {
	return w.session.outcome()
}

const (
	heartbeatInterval = 2 * time.Second
	heartbeatExpiry   = 15 * time.Second
	heartbeatGrace    = 30 * time.Second
)

type windowSession struct {
	server     *Server
	id         string
	intakePath string
	mode       string // intake | library
	created    time.Time
	seen       time.Time
	activity   time.Time
	open       bool
	result     *Result
	done       chan struct{}
	closer     func() error
}

func (w *windowSession) outcome() Result {
	w.server.mu.Lock()
	defer w.server.mu.Unlock()
	if w.result == nil {
		return Result{}
	}
	return *w.result
}

// Server owns the loopback panel service and its browser windows. Window
// results and liveness live only in memory; the service carries no workflow
// authority of its own.
type Server struct {
	backend  Backend
	listener net.Listener
	token    string
	handler  http.Handler
	mu       sync.Mutex
	windows  map[string]*windowSession
	now      func() time.Time
	started  time.Time
	// emptyTimeout bounds a service with no windows so a spawned serve-only
	// owner cannot orphan when its caller disappears before opening one.
	emptyTimeout time.Duration
}

// NewServer binds a loopback listener on an ephemeral port and mints a
// per-start secret token that guards writes.
func NewServer(backend Backend) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		listener.Close()
		return nil, err
	}
	s := &Server{
		backend:  backend,
		listener: listener,
		token:    hex.EncodeToString(token),
		windows:  map[string]*windowSession{},
		now:      time.Now,
		started:  time.Now(),
	}
	h := &handler{backend: backend, host: listener.Addr().String(), token: s.token, server: s}
	mux := http.NewServeMux()
	h.registerRoutes(mux)
	h.registerWindowRoutes(mux)
	h.registerAssetRoutes(mux)
	s.handler = mux
	return s, nil
}

// Addr is the loopback host:port the service bound.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// SetEmptyTimeout makes a windowless service exit once no window was ever
// registered for the duration; zero keeps serving until ctx ends.
func (s *Server) SetEmptyTimeout(d time.Duration) {
	s.mu.Lock()
	s.emptyTimeout = d
	s.mu.Unlock()
}

// NewWindow registers a window. A nonempty intake path reuses a live window
// opened for the same candidate; an empty path opens a library/run window.
func (s *Server) NewWindow(intakePath string) (Window, error) {
	w, _, err := s.openWindow(intakePath)
	return w, err
}

// openWindow registers or reuses a window and reports whether an existing
// live window answered the request.
func (s *Server) openWindow(intakePath string) (Window, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if intakePath != "" {
		for _, w := range s.windows {
			if w.open && w.intakePath == intakePath {
				return Window{ID: w.id, URL: s.windowURL(w.id), Done: w.done, session: w}, true, nil
			}
		}
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Window{}, false, err
	}
	mode := "library"
	if intakePath != "" {
		mode = "intake"
	}
	now := s.now()
	w := &windowSession{
		server:     s,
		id:         hex.EncodeToString(id),
		intakePath: intakePath,
		mode:       mode,
		created:    now,
		activity:   now,
		open:       true,
		done:       make(chan struct{}),
	}
	s.windows[w.id] = w
	return Window{ID: w.id, URL: s.windowURL(w.id), Done: w.done, session: w}, false, nil
}

func (s *Server) windowURL(id string) string {
	return "http://" + s.listener.Addr().String() + "/?window=" + id
}

// launchBrowser starts the service-owned managed browser for a freshly
// opened window and attaches its closer, so every window end — close,
// selection, timeout, heartbeat loss, or service shutdown — stops exactly
// that window's browser and removes its per-window profile directory.
// A reused window already has its browser; callers must not call this twice
// for one window.
func (s *Server) launchBrowser(w Window) error {
	browser, err := FindBrowser()
	if err != nil {
		return err
	}
	profile, err := BrowserProfileDir(s.backend.Root, w.ID)
	if err != nil {
		return err
	}
	cmd, err := browser.LaunchBrowser(w.URL, profile)
	if err != nil {
		return err
	}
	proc, err := harness.StartOwnedProcess(cmd)
	if err != nil {
		return err
	}
	if err := s.AttachWindowCloser(w.ID, func() error {
		return errors.Join(proc.Stop(), os.RemoveAll(profile))
	}); err != nil {
		return errors.Join(err, proc.Stop(), os.RemoveAll(profile))
	}
	return nil
}

// AttachWindowCloser associates a browser-process closer with one window.
// When the window is already closed the closer runs immediately.
func (s *Server) AttachWindowCloser(id string, close func() error) error {
	s.mu.Lock()
	w, known := s.windows[id]
	open := known && w.open
	if open {
		w.closer = close
	}
	s.mu.Unlock()
	if !known {
		return fmt.Errorf("unknown window %q", id)
	}
	if !open {
		return close()
	}
	return nil
}

// Serve runs the HTTP service and the window sweep until the context ends or
// every registered window has closed; shutdown closes owned windows and the
// listener.
func (s *Server) Serve(ctx context.Context) error {
	srv := &http.Server{Handler: s.handler}
	failed := make(chan error, 1)
	go func() {
		if err := srv.Serve(s.listener); err != http.ErrServerClosed {
			failed <- err
			return
		}
		failed <- nil
	}()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.closeAll()
			srv.Close()
			<-failed
			return ctx.Err()
		case err := <-failed:
			s.closeAll()
			srv.Close()
			return err
		case <-ticker.C:
			s.sweep()
			if s.allClosed() || s.emptyTimedOut() {
				srv.Shutdown(context.Background())
				<-failed
				return nil
			}
		}
	}
}

// allClosed reports whether at least one window was registered and every one
// has closed. A service with no windows keeps serving until its context ends.
func (s *Server) allClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.windows) == 0 {
		return false
	}
	for _, w := range s.windows {
		if w.open {
			return false
		}
	}
	return true
}

// emptyTimedOut reports a serve-only owner that never received a window
// before its startup grace expired.
func (s *Server) emptyTimedOut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emptyTimeout > 0 && len(s.windows) == 0 && s.now().Sub(s.started) > s.emptyTimeout
}

// resolveLocked records the caller's result exactly once; caller holds s.mu.
func (s *Server) resolveLocked(w *windowSession, res Result) {
	if w.result != nil {
		return
	}
	w.result = &res
	close(w.done)
}

// closeWindow resolves the result, marks the window closed, and runs the
// attached closer once. The caller must not hold s.mu.
func (s *Server) closeWindow(w *windowSession, result Result) {
	s.mu.Lock()
	if !w.open {
		s.mu.Unlock()
		return
	}
	w.open = false
	closer := w.closer
	s.resolveLocked(w, result)
	s.mu.Unlock()
	if closer != nil {
		closer()
	}
}

// sweep applies heartbeat expiry, user-idle timeout, and intake freshness to
// every open window at the injectable clock.
func (s *Server) sweep() {
	now := s.now()
	idle := s.idleTimeout()
	s.mu.Lock()
	var expired []*windowSession
	var timedOut []*windowSession
	var supersedeCheck []*windowSession
	for _, w := range s.windows {
		if !w.open {
			continue
		}
		base, limit := w.seen, heartbeatExpiry
		if w.seen.IsZero() {
			base, limit = w.created, heartbeatGrace
		}
		if now.Sub(base) > limit {
			expired = append(expired, w)
			continue
		}
		if idle > 0 && now.Sub(w.activity) > idle {
			timedOut = append(timedOut, w)
			continue
		}
		if w.mode == "intake" && w.result == nil {
			supersedeCheck = append(supersedeCheck, w)
		}
	}
	s.mu.Unlock()
	for _, w := range expired {
		s.closeWindow(w, s.endResult(w, "cancelled"))
	}
	for _, w := range timedOut {
		s.closeWindow(w, s.timeoutResult(w))
	}
	for _, w := range supersedeCheck {
		check, err := s.backend.CheckIntakeFreshness(w.intakePath)
		if err == nil && check.Superseded {
			s.mu.Lock()
			if w.open && w.result == nil {
				s.resolveLocked(w, Result{Status: "superseded", Reason: check.Reason})
				w.mode = "library"
			}
			s.mu.Unlock()
		}
	}
}

// endResult names the result for a window closing without a selection:
// cancelled while a configure caller waits on intake, closed otherwise.
func (s *Server) endResult(w *windowSession, intake string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.mode == "intake" && w.result == nil {
		return Result{Status: intake}
	}
	return Result{Status: "closed"}
}

// timeoutResult resolves an intake timeout to the current active pointer's
// target and its derived route; an unreadable or invalid active value reports
// the reason instead of inventing a selection.
func (s *Server) timeoutResult(w *windowSession) Result {
	s.mu.Lock()
	mode := w.mode
	s.mu.Unlock()
	if mode != "intake" {
		return Result{Status: "closed"}
	}
	state, err := s.backend.State()
	if err != nil {
		return Result{Status: "timeout", Reason: err.Error()}
	}
	if state.Error != "" {
		return Result{Status: "timeout", Reason: state.Error}
	}
	for _, option := range state.Configurations {
		if !option.Active {
			continue
		}
		route, err := facade.ConfigurationRoute(option.Configuration)
		if err != nil {
			return Result{Status: "timeout", Reason: err.Error()}
		}
		disabled := []string{}
		if option.Configuration.FlowSelection != nil {
			disabled = option.Configuration.FlowSelection.Disabled
		}
		return Result{Status: "timeout", Configuration: option.Name, Disabled: disabled, Route: route}
	}
	return Result{Status: "timeout", Reason: "no active configuration"}
}

// idleTimeout reads panelIdleTimeout from the current settings; zero disables
// the user-idle timeout only, never heartbeat expiry.
func (s *Server) idleTimeout() time.Duration {
	effective, err := settings.Load(s.backend.Root, s.backend.Home)
	if err != nil {
		return 0
	}
	return time.Duration(effective.Values.PanelIdleTimeout) * time.Second
}

// closeAll shuts every open window with the end result for its mode.
func (s *Server) closeAll() {
	s.mu.Lock()
	windows := make([]*windowSession, 0, len(s.windows))
	for _, w := range s.windows {
		if w.open {
			windows = append(windows, w)
		}
	}
	s.mu.Unlock()
	for _, w := range windows {
		s.closeWindow(w, s.endResult(w, "cancelled"))
	}
}

// session looks up one open window by id.
func (s *Server) session(id string) (*windowSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[id]
	return w, ok && w.open
}
