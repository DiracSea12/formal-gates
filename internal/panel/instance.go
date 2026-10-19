// Per-project panel service singleton: the kernel lock on
// .gates/panel-instance.lock decides ownership for the service's whole
// lifetime; panel-instance.json only describes the live owner to delegates.
package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"formal-gates/internal/coordination"
)

// Instance describes one live panel service to lifecycle clients. The lock,
// not this file, is ownership truth; a stale record is rewritten by the next
// owner while it holds the lock.
type Instance struct {
	PID   int    `json:"pid"`
	Addr  string `json:"addr"`
	Token string `json:"token"`
}

// WindowDescriptor is what /api/windows/open and PrepareService callers need
// to reach or reuse one panel window.
type WindowDescriptor struct {
	Window string `json:"window"`
	URL    string `json:"url"`
	Reused bool   `json:"reused"`
}

// CanonicalRoot normalizes a project root for singleton identity: absolute
// path plus symlink resolution where the platform supports it.
func CanonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs, nil
}

func instancePaths(root string) (lock, record string) {
	return filepath.Join(root, ".gates", "panel-instance.lock"),
		filepath.Join(root, ".gates", "panel-instance.json")
}

// InstanceLockPath is the canonical singleton lock path for a project root,
// exported so owners outside this package can probe the same lock object.
func InstanceLockPath(root string) string {
	lock, _ := instancePaths(root)
	return lock
}

// ServiceHandle is one caller's view of the per-project service: either the
// owner (Server non-nil) or a delegate that only holds the descriptor.
type ServiceHandle struct {
	Instance     Instance
	Server       *Server
	instancePath string
	unlock       func()
}

// Owned reports whether this process runs the service.
func (h *ServiceHandle) Owned() bool { return h.Server != nil }

// Release is idempotent cleanup for an owner after its Serve loop returns:
// the record is removed only while this service still holds the lock.
func (h *ServiceHandle) Release() {
	if h.unlock == nil {
		return
	}
	if h.instancePath != "" {
		_ = os.Remove(h.instancePath)
	}
	h.unlock()
	h.unlock = nil
}

// PrepareService makes this process the per-project panel service owner, or
// waits for the descriptor of the live owner and returns a delegate handle.
// Lock contention is decided by the kernel: a held lock means a live owner is
// starting or serving, so delegates only wait for its record to become usable;
// if that owner dies before publishing one, the waiter takes the same lock and
// becomes the owner itself.
func PrepareService(ctx context.Context, backend Backend) (*ServiceHandle, error) {
	root, err := CanonicalRoot(backend.Root)
	if err != nil {
		return nil, err
	}
	backend.Root = root
	lockPath, recordPath := instancePaths(root)
	unlock, err := coordination.AcquirePath(lockPath, "panel service")
	if err != nil {
		var held *coordination.LockHeldError
		if !errors.As(err, &held) {
			return nil, err
		}
		var instance Instance
		instance, unlock, err = WaitOrAcquire(ctx, root)
		if err != nil {
			return nil, err
		}
		if unlock == nil {
			return &ServiceHandle{Instance: instance}, nil
		}
	}
	return ownService(backend, unlock, recordPath)
}

// ownService starts the in-process service under an already-held lock and
// publishes its descriptor record.
func ownService(backend Backend, unlock func(), recordPath string) (*ServiceHandle, error) {
	server, err := NewServer(backend)
	if err != nil {
		unlock()
		return nil, err
	}
	handle := &ServiceHandle{
		Instance:     Instance{PID: os.Getpid(), Addr: server.Addr(), Token: server.token},
		Server:       server,
		instancePath: recordPath,
		unlock:       unlock,
	}
	// The lock is held, so any earlier record belongs to a dead owner of this
	// same service path; replace it, never another live owner's.
	body, err := json.Marshal(handle.Instance)
	if err != nil {
		server.listener.Close()
		unlock()
		return nil, err
	}
	if err := os.WriteFile(recordPath, body, 0o600); err != nil {
		server.listener.Close()
		unlock()
		return nil, err
	}
	return handle, nil
}

// WaitOrAcquire waits for the live owner's descriptor. If the starting owner
// dies before its record becomes usable, the waiter acquires the same kernel
// lock itself and returns it — the caller then takes over through the normal
// owner path instead of waiting on a dead record forever.
func WaitOrAcquire(ctx context.Context, root string) (Instance, func(), error) {
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return Instance{}, nil, err
	}
	lockPath, recordPath := instancePaths(canonical)
	for {
		body, err := os.ReadFile(recordPath)
		if err == nil {
			var instance Instance
			if json.Unmarshal(body, &instance) == nil && instance.Addr != "" && instance.Token != "" {
				if instanceReady(ctx, instance) {
					return instance, nil, nil
				}
			}
		}
		// A free lock means the starting owner is gone; the kernel decides.
		unlock, err := coordination.AcquirePath(lockPath, "panel service")
		if err == nil {
			return Instance{}, unlock, nil
		}
		var held *coordination.LockHeldError
		if !errors.As(err, &held) {
			return Instance{}, nil, err
		}
		select {
		case <-ctx.Done():
			return Instance{}, nil, fmt.Errorf("panel service instance was not ready: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// OpenWindowOn opens or reuses a window on an instance this process does not
// own, using the descriptor's loopback address and token.
func OpenWindowOn(ctx context.Context, instance Instance, intakePath string) (WindowDescriptor, error) {
	return (&ServiceHandle{Instance: instance}).OpenWindow(ctx, intakePath)
}

// WindowStatusOn reads one delegated window's open flag and result.
func WindowStatusOn(ctx context.Context, instance Instance, id string) (bool, *Result, error) {
	return (&ServiceHandle{Instance: instance}).WindowStatus(ctx, id)
}

// instanceReady confirms the recorded process actually serves on its own
// address: the token-authenticated identity endpoint answers regardless of
// whether the project's configuration happens to parse right now, so a bad
// config file cannot masquerade as a dead service.
func instanceReady(ctx context.Context, instance Instance) bool {
	request, err := http.NewRequestWithContext(ctx, "GET", "http://"+instance.Addr+"/api/instance", nil)
	if err != nil {
		return false
	}
	request.Host = instance.Addr
	request.Header.Set("X-Formal-Gates-Token", instance.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var identity struct {
		PID int `json:"pid"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&identity) != nil {
		return false
	}
	return identity.PID == instance.PID
}

// Serve runs the owned service until ctx ends, every window closes, or the
// empty-service grace expires; on return the record and lock are released.
func (h *ServiceHandle) Serve(ctx context.Context) error {
	defer h.Release()
	return h.Server.Serve(ctx)
}

// OpenWindow opens or reuses one window through the guarded lifecycle
// endpoint. An empty intake opens a library/run window; the same intake
// candidate returns its existing window with Reused set.
func (h *ServiceHandle) OpenWindow(ctx context.Context, intakePath string) (WindowDescriptor, error) {
	body, err := json.Marshal(map[string]string{"intake": intakePath})
	if err != nil {
		return WindowDescriptor{}, err
	}
	request, err := http.NewRequestWithContext(ctx, "POST",
		"http://"+h.Instance.Addr+"/api/windows/open", strings.NewReader(string(body)))
	if err != nil {
		return WindowDescriptor{}, err
	}
	request.Host = h.Instance.Addr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+h.Instance.Addr)
	request.Header.Set("X-Formal-Gates-Token", h.Instance.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return WindowDescriptor{}, err
	}
	defer response.Body.Close()
	var reply struct {
		WindowDescriptor
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return WindowDescriptor{}, err
	}
	if response.StatusCode != http.StatusOK {
		return WindowDescriptor{}, fmt.Errorf("open window: %s", reply.Error)
	}
	return reply.WindowDescriptor, nil
}

// CloseWindow asks the service to resolve and close one window; lifecycle
// clients use it when their own side cannot keep the window usable.
func (h *ServiceHandle) CloseWindow(ctx context.Context, id string) error {
	body, err := json.Marshal(map[string]string{"window": id})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST",
		"http://"+h.Instance.Addr+"/api/window/close", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Host = h.Instance.Addr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+h.Instance.Addr)
	request.Header.Set("X-Formal-Gates-Token", h.Instance.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("close window: status %d", response.StatusCode)
	}
	return nil
}

// WindowStatus reads one window's open flag and resolved result.
func (h *ServiceHandle) WindowStatus(ctx context.Context, id string) (bool, *Result, error) {
	request, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+h.Instance.Addr+"/api/window?window="+id, nil)
	if err != nil {
		return false, nil, err
	}
	request.Host = h.Instance.Addr
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, nil, err
	}
	defer response.Body.Close()
	var view struct {
		Open   bool    `json:"open"`
		Result *Result `json:"result"`
		Error  string  `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		return false, nil, err
	}
	if response.StatusCode != http.StatusOK {
		return false, nil, fmt.Errorf("window status: %s", view.Error)
	}
	return view.Open, view.Result, nil
}
