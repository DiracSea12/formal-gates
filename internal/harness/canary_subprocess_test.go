//go:build !windows

package harness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCanarySubprocessSettlesPrivateDescendants(t *testing.T) {
	for _, mode := range []string{"normal", "cancelled", "inherited-pipe"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			pidfile := filepath.Join(root, "child.pid")
			script := `sleep 30 &
child=$!
printf '%s\n' "$child" > "$1"
trap 'kill "$child" 2>/dev/null; wait "$child"; exit 0' TERM
case "$2" in
normal) kill "$child"; wait "$child"; exit 0;;
inherited-pipe) exit 0;;
cancelled) wait "$child";;
esac`
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() { waitForFile(t, pidfile); cancel() }()
			}

			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script, "peer", pidfile, mode)
			started := time.Now()
			_, err := canarySubprocessOutput(cmd)
			if mode == "normal" && err != nil {
				t.Fatal(err)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation result=%v", err)
			}
			if mode == "inherited-pipe" && !errors.Is(err, exec.ErrWaitDelay) {
				t.Fatalf("pipe result=%v", err)
			}
			body, readErr := os.ReadFile(pidfile)
			if readErr != nil {
				t.Fatal(readErr)
			}
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(body)))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			terminated := false
			for until := time.Now().Add(time.Second); time.Now().Before(until); {
				if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					terminated = true
					break
				}
				status, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
				if strings.HasPrefix(strings.TrimSpace(string(status)), "Z") {
					terminated = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !terminated {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("private descendant %d remains executing", pid)
			}
			t.Logf("mode=%s descendant=%d terminated=true elapsed=%s", mode, pid, time.Since(started))
		})
	}
}

func TestCanaryPrivateP4PIDFileOwnsBackendCleanup(t *testing.T) {
	binary, err := exec.LookPath("p4d")
	if err != nil {
		t.Skip("p4d is not installed")
	}
	for _, mode := range []string{"normal", "interrupted-owner"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			pidFile := filepath.Join(root, "p4d.pid")
			server := exec.Command(binary, "--pid-file="+pidFile, "-r", root, "-p", "127.0.0.1:0")
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
			var pid int
			for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
				if body, err := os.ReadFile(pidFile); err == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(body)))
					if pid > 0 {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid != server.Process.Pid {
				t.Fatalf("native pid file=%d started process=%d", pid, server.Process.Pid)
			}
			pgid, err := syscall.Getpgid(pid)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "normal" {
				_ = server.Process.Kill()
				_ = server.Wait()
				if err = os.Remove(pidFile); err != nil {
					t.Fatal(err)
				}
			}
			canaryStopPrivateBackend(t, pidFile)
			if mode != "normal" {
				if err = server.Wait(); err == nil {
					t.Fatal("expected native backend to be signalled")
				}
			}
			if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("private backend %d still exists: %v", pid, err)
			}
			t.Logf("native pid=%d pgid=%d mode=%s terminated=true", pid, pgid, mode)
		})
	}
}
