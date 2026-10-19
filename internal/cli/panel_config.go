package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"formal-gates/internal/panel"
)

// emptyServiceGrace bounds a spawned serve-only service that never received a
// window, so a disappeared caller cannot orphan it.
const emptyServiceGrace = 30 * time.Second

// runConfig is the public flow-configuration entry: it runs or delegates the
// per-project panel service, opens a library window in a managed browser, and
// waits for the window's result. --serve --owned is the internal service-only
// form that lifecycle hosts spawn; it exits when its stdin pipe closes.
func runConfig(args []string, streams IO) (int, error) {
	fs := newFlagSet("config", streams)
	root := fs.String("root", ".", "project root")
	serve := fs.Bool("serve", false, "run only the panel service and print its ready descriptor")
	owned := fs.Bool("owned", false, "exit when the stdin owner pipe closes")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	if *serve {
		return runConfigServe(*root, home, *owned, streams)
	}
	return runConfigPanel(*root, home, streams)
}

// serveContext is the owner's lifetime: Ctrl+C ends a foreground service, and
// --owned stdin EOF ends a spawned one. A terminal's plain stdin EOF is not a
// shutdown signal, so only --owned watches it.
func serveContext(streams IO, owned bool) (context.Context, context.CancelFunc) {
	parent := streams.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt)
	if owned {
		go func() {
			_, _ = io.Copy(io.Discard, streams.Stdin)
			cancel()
		}()
	}
	return ctx, cancel
}

// runConfigServe acquires-or-delegates the singleton. As delegate it prints
// the live descriptor and exits; as owner it prints its own descriptor and
// serves until the context ends or the empty-startup grace expires.
func runConfigServe(root, home string, owned bool, streams IO) (int, error) {
	ctx, cancel := serveContext(streams, owned)
	defer cancel()
	handle, err := panel.PrepareService(ctx, panel.Backend{Root: root, Home: home})
	if err != nil {
		return 1, err
	}
	defer handle.Release()
	if code, err := printJSON(streams.Stdout, map[string]any{
		"ready":     true,
		"delegated": !handle.Owned(),
		"pid":       handle.Instance.PID,
		"addr":      handle.Instance.Addr,
		"token":     handle.Instance.Token,
	}); err != nil {
		return code, err
	}
	if !handle.Owned() {
		return 0, nil
	}
	handle.Server.SetEmptyTimeout(emptyServiceGrace)
	if err := handle.Serve(ctx); err != nil && ctx.Err() == nil {
		return 1, err
	}
	return 0, nil
}

// runConfigPanel is the public UX: a creator serves in the foreground, a
// repeated invocation delegates to the live instance and opens a fresh
// library/run window on it. The panelSelection switch does not apply here —
// running `config` is itself the explicit request.
func runConfigPanel(root, home string, streams IO) (int, error) {
	ctx, cancel := serveContext(streams, false)
	defer cancel()
	canonical, err := panel.CanonicalRoot(root)
	if err != nil {
		return 1, err
	}
	handle, err := panel.PrepareService(ctx, panel.Backend{Root: canonical, Home: home})
	if err != nil {
		return 1, err
	}
	defer handle.Release()
	if !handle.Owned() {
		return delegatePanelWindow(ctx, handle, streams)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- handle.Serve(ctx) }()
	// The service owns the managed browser launched by the open endpoint.
	if _, err := handle.OpenWindow(ctx, ""); err != nil {
		cancel()
		<-serveDone
		return 1, err
	}
	err = <-serveDone
	if ctx.Err() != nil || err == nil {
		return 0, nil
	}
	return 1, err
}

// delegatePanelWindow opens a new window on an existing service, waits for
// its result, and prints it; the delegate never owns or stops the service.
func delegatePanelWindow(ctx context.Context, handle *panel.ServiceHandle, streams IO) (int, error) {
	desc, err := handle.OpenWindow(ctx, "")
	if err != nil {
		return 1, err
	}
	fmt.Fprintf(streams.Stdout, "panel window %s\n", desc.URL)
	for {
		open, result, err := handle.WindowStatus(ctx, desc.Window)
		if err != nil {
			return 1, err
		}
		if result != nil {
			return printJSON(streams.Stdout, result)
		}
		if !open {
			return printJSON(streams.Stdout, panel.Result{Status: "closed"})
		}
		select {
		case <-ctx.Done():
			return 1, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
