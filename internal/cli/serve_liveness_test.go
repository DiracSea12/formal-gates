package cli

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// testLivenessDeps returns deps with millisecond probes so the supervisor's
// real ticker logic runs at test speed.
func testLivenessDeps() serveLivenessDeps {
	return serveLivenessDeps{
		ping:            func(context.Context) error { return nil },
		parentAlive:     func(int) bool { return true },
		onDead:          func() {},
		parentPID:       4321,
		parentInterval:  time.Millisecond,
		pingInterval:    time.Millisecond,
		pingTimeout:     5 * time.Millisecond,
		maxPingFailures: 3,
	}
}

// A host that never answers pings dies after the configured consecutive
// failures; each probe carries the guard's deadline.
func TestServeLivenessUnansweredPingsKillServe(t *testing.T) {
	deps := testLivenessDeps()
	dead := make(chan struct{})
	deps.onDead = func() { close(dead) }
	deps.ping = func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("ping probe has no deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	guard := newServeLivenessGuard(deps)
	guard.Start()
	defer guard.Stop()
	select {
	case <-dead:
	case <-time.After(10 * time.Second):
		t.Fatal("unanswered pings did not kill the serve")
	}
}

// Every third probe answers, so the failure streak never reaches the limit
// and the serve stays up while the parent is alive.
func TestServeLivenessAnsweredPingsKeepServeAlive(t *testing.T) {
	deps := testLivenessDeps()
	dead := make(chan struct{})
	deps.onDead = func() { close(dead) }
	var calls atomic.Int32
	deps.ping = func(context.Context) error {
		if calls.Add(1)%3 == 0 {
			return nil
		}
		return errors.New("unanswered")
	}
	guard := newServeLivenessGuard(deps)
	guard.Start()
	defer guard.Stop()
	select {
	case <-dead:
		t.Fatal("answered pings still killed the serve")
	case <-time.After(200 * time.Millisecond):
	}
	if calls.Load() < 6 {
		t.Fatal("the guard did not keep probing", calls.Load())
	}
}

// A dead parent process kills the serve on the next parent check, and the
// probe sees the PID captured at start.
func TestServeLivenessDeadParentKillsServe(t *testing.T) {
	deps := testLivenessDeps()
	dead := make(chan struct{})
	deps.onDead = func() { close(dead) }
	var probed atomic.Int64
	deps.parentAlive = func(pid int) bool {
		probed.Store(int64(pid))
		return false
	}
	guard := newServeLivenessGuard(deps)
	guard.Start()
	defer guard.Stop()
	select {
	case <-dead:
	case <-time.After(10 * time.Second):
		t.Fatal("a dead parent did not kill the serve")
	}
	if got := probed.Load(); got != int64(deps.parentPID) {
		t.Fatal("the guard probed a different parent pid", got, deps.parentPID)
	}
	guard.Stop()
}

// Start and Stop are idempotent, and Stop reclaims the supervisor goroutine
// when the serve exits normally.
func TestServeLivenessGuardStartStopIdempotent(t *testing.T) {
	guard := newServeLivenessGuard(testLivenessDeps())
	guard.Start()
	guard.Start()
	stopped := make(chan struct{})
	go func() {
		guard.Stop()
		guard.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not reclaim the supervisor goroutine")
	}
}
