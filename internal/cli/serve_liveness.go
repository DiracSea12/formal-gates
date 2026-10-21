package cli

import (
	"context"
	"os"
	"sync"
	"time"

	"formal-gates/internal/coordination"
)

const (
	serveLivenessParentInterval  = 30 * time.Second
	serveLivenessPingInterval    = 60 * time.Second
	serveLivenessPingTimeout     = 10 * time.Second
	serveLivenessMaxPingFailures = 3
)

// serveLivenessDeps injects everything the guard probes; the guard holds no
// serve or transport state. stdin EOF is already fatal inside the transport,
// so the remaining death signals are a dead parent and unanswered pings.
type serveLivenessDeps struct {
	ping        func(ctx context.Context) error
	parentAlive func(pid int) bool
	onDead      func()

	parentPID       int
	parentInterval  time.Duration
	pingInterval    time.Duration
	pingTimeout     time.Duration
	maxPingFailures int
}

// serveLivenessGuard supervises one serve: a dead parent process or
// consecutive unanswered host pings ends it via onDead.
type serveLivenessGuard struct {
	deps    serveLivenessDeps
	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newServeLivenessGuard(deps serveLivenessDeps) *serveLivenessGuard {
	if deps.parentAlive == nil {
		deps.parentAlive = coordination.ProcessAlive
	}
	if deps.parentInterval <= 0 {
		deps.parentInterval = serveLivenessParentInterval
	}
	if deps.pingInterval <= 0 {
		deps.pingInterval = serveLivenessPingInterval
	}
	if deps.pingTimeout <= 0 {
		deps.pingTimeout = serveLivenessPingTimeout
	}
	if deps.maxPingFailures <= 0 {
		deps.maxPingFailures = serveLivenessMaxPingFailures
	}
	return &serveLivenessGuard{deps: deps}
}

// Start launches the single supervisor goroutine. The parent PID is captured
// once because repeated os.Getppid calls can read stale cached values.
func (g *serveLivenessGuard) Start() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return
	}
	g.started = true
	if g.deps.parentPID == 0 {
		g.deps.parentPID = os.Getppid()
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.wg.Add(1)
	go g.run(ctx)
}

// Stop cancels the supervisor and waits for its goroutine to exit.
func (g *serveLivenessGuard) Stop() {
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		return
	}
	g.started = false
	cancel := g.cancel
	g.mu.Unlock()
	cancel()
	g.wg.Wait()
}

func (g *serveLivenessGuard) run(ctx context.Context) {
	defer g.wg.Done()
	parent := time.NewTicker(g.deps.parentInterval)
	defer parent.Stop()
	ping := time.NewTicker(g.deps.pingInterval)
	defer ping.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-parent.C:
			if !g.deps.parentAlive(g.deps.parentPID) {
				g.deps.onDead()
				return
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, g.deps.pingTimeout)
			err := g.deps.ping(pingCtx)
			cancel()
			if err == nil {
				failures = 0
			} else if failures++; failures >= g.deps.maxPingFailures {
				g.deps.onDead()
				return
			}
		}
	}
}
