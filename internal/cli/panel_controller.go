package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"formal-gates/internal/coordination"
	"formal-gates/internal/harness"
	"formal-gates/internal/panel"
	"formal-gates/internal/settings"
)

// panelController owns panel service children for one MCP server across
// configure calls. A service it spawns runs as `config --serve --owned` with
// a parent-kept stdin pipe; cancelling one configure cancels only its result
// wait, never the service. Services owned by other processes are reused as
// delegates without taking their ownership.
type panelController struct {
	binary   string
	mu       sync.Mutex
	services map[string]*controlledService
	closed   bool
}

type controlledService struct {
	proc       *harness.OwnedProcess
	stdin      io.Closer
	errbuf     *lockedBuffer
	descriptor panel.Instance
}

// lockedBuffer keeps a spawned service's stderr for failure reporting.
type lockedBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < 8192 {
		b.buf = append(b.buf, p...)
	}
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func newPanelController() (*panelController, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &panelController{binary: binary, services: map[string]*controlledService{}}, nil
}

// Close ends every owned service by closing its stdin owner pipe, then joins
// all children exactly once. Browsers belong to the service process; they die
// with it through the owned-process tree.
func (c *panelController) Close() {
	c.mu.Lock()
	c.closed = true
	services := make([]*controlledService, 0, len(c.services))
	for _, svc := range c.services {
		services = append(services, svc)
	}
	c.services = map[string]*controlledService{}
	c.mu.Unlock()
	for _, svc := range services {
		_ = svc.stdin.Close()
	}
	for _, svc := range services {
		_ = svc.proc.Join()
	}
}

// ensureService returns the live per-project instance: an already-owned
// child, a delegate descriptor when another owner holds the lock, or a newly
// spawned `config --serve --owned` child this controller then owns.
func (c *panelController) ensureService(ctx context.Context, root string) (panel.Instance, *controlledService, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return panel.Instance{}, nil, fmt.Errorf("panel controller is closed")
	}
	if svc, ok := c.services[root]; ok {
		select {
		case <-svc.proc.Done():
			// The owned child exited on its own (idle timeout, last window
			// closed); evict it and re-enter the normal ensure path.
			delete(c.services, root)
			c.mu.Unlock()
			_ = svc.stdin.Close()
			_ = svc.proc.Join()
			goto resolve
		default:
			c.mu.Unlock()
			return svc.instance(), svc, nil
		}
	}
	c.mu.Unlock()

resolve:
	unlock, err := coordination.AcquirePath(panel.InstanceLockPath(root), "panel service probe")
	if err != nil {
		var held *coordination.LockHeldError
		if !errors.As(err, &held) {
			return panel.Instance{}, nil, err
		}
		// Another owner is starting or serving; wait for its descriptor or
		// take over if it dies before publishing one.
		instance, acquired, err := panel.WaitOrAcquire(ctx, root)
		if err != nil {
			return panel.Instance{}, nil, err
		}
		if acquired == nil {
			return instance, nil, nil
		}
		// The starting owner is gone; this caller owns the lock now, but the
		// spawned service re-acquires it for its real lifetime.
		acquired()
	} else {
		// The probe only reads the kernel's answer; the spawned service
		// re-acquires the lock for its real lifetime.
		unlock()
	}

	cmd := exec.Command(c.binary, "config", "--serve", "--owned", "--root", root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return panel.Instance{}, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return panel.Instance{}, nil, err
	}
	errbuf := &lockedBuffer{}
	cmd.Stderr = errbuf
	proc, err := harness.StartOwnedProcess(cmd)
	if err != nil {
		return panel.Instance{}, nil, err
	}
	svc := &controlledService{proc: proc, stdin: stdin, errbuf: errbuf}
	var descriptor struct {
		Ready     bool   `json:"ready"`
		Delegated bool   `json:"delegated"`
		PID       int    `json:"pid"`
		Addr      string `json:"addr"`
		Token     string `json:"token"`
	}
	decode := make(chan error, 1)
	go func() {
		decode <- json.NewDecoder(stdout).Decode(&descriptor)
	}()
	select {
	case err := <-decode:
		if err != nil {
			_ = stdin.Close()
			_ = proc.Join()
			return panel.Instance{}, nil, fmt.Errorf("panel service did not report ready: %v %s", err, errbuf.String())
		}
	case <-ctx.Done():
		_ = stdin.Close()
		_ = proc.Join()
		return panel.Instance{}, nil, ctx.Err()
	}
	instance := panel.Instance{PID: descriptor.PID, Addr: descriptor.Addr, Token: descriptor.Token}
	if descriptor.Delegated {
		// The spawned process delegated and is exiting on its own.
		_ = stdin.Close()
		_ = proc.Join()
		return instance, nil, nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = stdin.Close()
		_ = proc.Join()
		return panel.Instance{}, nil, fmt.Errorf("panel controller is closed")
	}
	svc.descriptor = instance
	c.services[root] = svc
	c.mu.Unlock()
	return instance, svc, nil
}

func (s *controlledService) instance() panel.Instance {
	return s.descriptor
}

// configure implements the MCP configure tool: ensure the project service,
// open (or reuse) the intake/library window, launch its managed browser, and
// wait for the window's result. It never starts, resets, or authorizes a run.
func (c *panelController) configure(ctx context.Context, root string, intake int, force bool) (panel.Result, error) {
	canonical, err := panel.CanonicalRoot(root)
	if err != nil {
		return panel.Result{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return panel.Result{}, err
	}
	if !force {
		effective, err := settings.Load(canonical, home)
		if err != nil {
			return panel.Result{}, err
		}
		if !effective.Values.PanelSelection {
			return panel.Result{Status: "disabled", Reason: "panelSelection is disabled; equivalent settings and conversation selection remain available, or run formal-gates config in a terminal"}, nil
		}
	}
	intakePath := ""
	if intake > 0 {
		selected, done, err := selectOriginalPayload(workflowPayloadDirectory(canonical, "", "intake"), intake, io.Discard)
		if err != nil {
			return panel.Result{}, err
		}
		if !done {
			return panel.Result{Status: "unavailable", Reason: "no saved intake candidates exist for this project"}, nil
		}
		intakePath = selected
	}
	instance, _, err := c.ensureService(ctx, canonical)
	if err != nil {
		return panel.Result{}, err
	}
	// The service launches and owns the managed browser for a fresh window;
	// an open failure (no desktop, no browser) reports the real reason.
	desc, err := panel.OpenWindowOn(ctx, instance, intakePath)
	if err != nil {
		return panel.Result{Status: "unavailable", Reason: err.Error()}, nil
	}
	open, result, err := panel.WaitWindowOn(ctx, instance, desc.Window)
	if ctx.Err() != nil {
		// Request cancellation detaches the waiter, not the panel window.
		return panel.Result{Status: "cancelled", Reason: "configure call cancelled; panel window remains open"}, nil
	}
	if err != nil {
		return panel.Result{}, err
	}
	if result != nil {
		return *result, nil
	}
	if !open {
		return panel.Result{Status: "closed"}, nil
	}
	return panel.Result{}, fmt.Errorf("panel wait ended without a window result")
}

func (c *panelController) selectConfiguration(ctx context.Context, root string, intake int, configuration string) error {
	canonical, err := panel.CanonicalRoot(root)
	if err != nil {
		return err
	}
	candidate, done, err := selectOriginalPayload(workflowPayloadDirectory(canonical, "", "intake"), intake, io.Discard)
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("no saved intake candidates exist for this project")
	}
	return panel.SelectOpenIntake(ctx, root, candidate, configuration)
}
