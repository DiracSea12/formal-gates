package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestOwnedProcessStopKillsDescendants proves the job/process-group ownership
// machinery: stopping the owned child also kills the descendant it spawned,
// not just the direct process. The helper roles run inside this same test
// binary; the grandchild heartbeats into a file so its death is observable.
func TestOwnedProcessStopKillsDescendants(t *testing.T) {
	if role := os.Getenv("OWNED_HELPER_ROLE"); role != "" {
		ownedHelper(role)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	beat := filepath.Join(t.TempDir(), "heartbeat")
	child := exec.Command(exe, "-test.run=TestOwnedProcessStopKillsDescendants$")
	child.Env = append(os.Environ(), "OWNED_HELPER_ROLE=child", "OWNED_BEAT="+beat)
	proc, err := StartOwnedProcess(child)
	if err != nil {
		t.Fatalf("start owned child: %v", err)
	}
	read := func() string {
		data, _ := os.ReadFile(beat)
		return string(data)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && read() == "" {
		time.Sleep(50 * time.Millisecond)
	}
	if read() == "" {
		_ = proc.Stop()
		t.Fatal("grandchild never started heartbeating")
	}
	// The child was killed; its own nonzero exit is expected.
	_ = proc.Stop()
	last := read()
	time.Sleep(1500 * time.Millisecond)
	if read() != last {
		t.Fatal("grandchild kept heartbeating after its owner was killed")
	}
}

func ownedHelper(role string) {
	switch role {
	case "childexit":
		// Spawn a descendant that inherits this child's stdout/stderr pipes,
		// then exit immediately so only the descendant keeps them open.
		exe, err := os.Executable()
		if err != nil {
			os.Exit(1)
		}
		grand := exec.Command(exe, "-test.run=TestOwnedProcessJoinFreesPipeHoldingDescendant$")
		grand.Env = append(os.Environ(), "OWNED_HELPER_ROLE=grandchild")
		grand.Stdout = os.Stdout
		grand.Stderr = os.Stderr
		if err := grand.Start(); err != nil {
			os.Exit(1)
		}
		for i := 0; i < 300; i++ {
			if data, err := os.ReadFile(os.Getenv("OWNED_BEAT")); err == nil && len(data) > 0 {
				os.Exit(0)
			}
			time.Sleep(50 * time.Millisecond)
		}
		os.Exit(1)
	case "child":
		exe, err := os.Executable()
		if err != nil {
			os.Exit(1)
		}
		grand := exec.Command(exe, "-test.run=TestOwnedProcessStopKillsDescendants$")
		grand.Env = append(os.Environ(), "OWNED_HELPER_ROLE=grandchild")
		grand.Stdout = os.Stderr
		grand.Stderr = os.Stderr
		if err := grand.Start(); err != nil {
			os.Exit(1)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "grandchild":
		path := os.Getenv("OWNED_BEAT")
		for {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				f.WriteString(time.Now().Format("15:04:05.000") + "\n")
				f.Close()
			}
			time.Sleep(80 * time.Millisecond)
		}
	}
}

// TestOwnedProcessJoinFreesPipeHoldingDescendant: the main child exits while
// a descendant still holds the inherited stdout pipe. Join must reap the main
// process, settle the tree, and never deadlock on the undrained pipe.
func TestOwnedProcessJoinFreesPipeHoldingDescendant(t *testing.T) {
	if role := os.Getenv("OWNED_HELPER_ROLE"); role != "" {
		ownedHelper(role)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	beat := filepath.Join(t.TempDir(), "heartbeat")
	child := exec.Command(exe, "-test.run=TestOwnedProcessJoinFreesPipeHoldingDescendant$")
	child.Env = append(os.Environ(), "OWNED_HELPER_ROLE=childexit", "OWNED_BEAT="+beat)
	proc, err := StartOwnedProcess(child)
	if err != nil {
		t.Fatalf("start owned child: %v", err)
	}
	// The child exits as soon as its descendant is heartbeating; the
	// descendant keeps the inherited stdout pipe open while it lives.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(beat); len(data) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if data, _ := os.ReadFile(beat); len(data) == 0 {
		_ = proc.Stop()
		t.Fatal("grandchild never started heartbeating")
	}
	done := make(chan error, 1)
	go func() { done <- proc.Join() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Join deadlocked on a descendant holding the child's output pipe")
	}
	// The descendant was killed with the tree: its heartbeats stop.
	last, _ := os.ReadFile(beat)
	time.Sleep(1500 * time.Millisecond)
	data, _ := os.ReadFile(beat)
	if string(data) != string(last) {
		t.Fatal("grandchild kept heartbeating after the owned tree settled")
	}
}
