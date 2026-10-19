package harness

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// OwnedProcess is one child started under the supervisor's ownership
// machinery: on Windows the child and its descendants die with the owner via
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; on POSIX the child runs in its own
// process group so the owner can kill the whole tree explicitly — a POSIX
// owner that vanishes without settling leaves descendants behind, which is
// why callers pair StartOwnedProcess with a caller lifetime like stdin EOF.
// The owner joins exactly once — Join for a self-exiting child, Stop to kill
// it first.
type OwnedProcess struct {
	Cmd    *exec.Cmd
	settle func() error
	exited chan error
	done   chan struct{}
	once   sync.Once
	result error
}

// StartOwnedProcess starts cmd with ownership. When the caller supplied no
// supervisor attributes the isolated group/job attributes are applied.
func StartOwnedProcess(cmd *exec.Cmd) (*OwnedProcess, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = supervisorSysProcAttr()
	}
	// Owned children hand the settle path stdout/stderr writers, so unset
	// sinks are initialized instead of being wrapped as nil.
	if cmd.Stdout == nil {
		cmd.Stdout = io.Discard
	}
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	// A descendant that inherits a piped output stream would keep Cmd.Wait
	// blocked past the main child's exit; WaitDelay bounds that drain so the
	// settle path (job close / group signal) can still reach the tree.
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 5 * time.Second
	}
	settle, err := startChild(cmd, true)
	if err != nil {
		return nil, err
	}
	p := &OwnedProcess{Cmd: cmd, settle: settle, exited: make(chan error, 1), done: make(chan struct{})}
	go func() {
		p.exited <- cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// Done closes when the child's own Wait finished. It is a broadcast
// observation point: any number of watchers can poll it, unlike the
// consumes-once exited channel Join reads its result from.
func (p *OwnedProcess) Done() <-chan struct{} {
	return p.done
}

// Join waits for the child's own exit, then settles its descendants.
func (p *OwnedProcess) Join() error {
	p.once.Do(func() {
		p.result = errors.Join(<-p.exited, p.settle())
	})
	return p.result
}

// Stop kills this child, then joins. Descendants are covered by the job or
// process group rather than by PID guessing.
func (p *OwnedProcess) Stop() error {
	if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Kill()
	}
	return p.Join()
}
