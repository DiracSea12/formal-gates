package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSupervisedCancellationPeer(t *testing.T) {
	directory := os.Getenv("FORMAL_GATES_CANCELLATION_PEER")
	if directory == "" {
		return
	}
	if _, err := os.Stdout.Write(bytes.Repeat([]byte("x"), 8<<20)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(os.Stderr, "original diagnostic"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "ready"), []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Hour)
}

func TestSuperviseCancellationPreservesRequestAndStreams(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, when := range []string{"identity", "output"} {
		t.Run(when, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			request := map[string]any{
				"kind": "approved-development-check", "argv": []string{executable, "-test.run=^TestSupervisedCancellationPeer$"},
				"cwd": directory, "stdin": "", "environment": map[string]any{"FORMAL_GATES_CANCELLATION_PEER": directory},
			}
			writeRequest(t, directory, request)
			original := readBytes(t, filepath.Join(directory, "request.json"))
			digest, err := digestOf(t, request)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(supervisorBinary(t), "harness", "supervise", directory)
			command.SysProcAttr = supervisorSysProcAttr()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			settled := false
			t.Cleanup(func() {
				if !settled {
					_ = command.Process.Kill()
					<-done
				}
			})
			marker := filepath.Join(directory, "process.json")
			if when == "output" {
				marker = filepath.Join(directory, "ready")
			}
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				select {
				case err := <-done:
					settled = true
					t.Fatalf("supervisor exited before %s: %v", when, err)
				default:
				}
				time.Sleep(time.Millisecond)
			}
			if err := terminateJob(command, command.Process.Pid); err != nil {
				t.Fatal(err)
			}
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for !settled {
				select {
				case err := <-done:
					settled = true
					if err != nil {
						t.Fatal("supervisor did not settle repeated cancellation:", err)
					}
				case <-ticker.C:
					_ = terminateJob(command, command.Process.Pid)
				}
			}
			receipt := readReceipt(t, directory)
			if receipt["exit_code"] != float64(130) || receipt["error"] != "owned job was cancelled" || receipt["request_digest"] != digest {
				t.Fatal("original cancellation receipt changed", receipt)
			}
			if !bytes.Equal(original, readBytes(t, filepath.Join(directory, "request.json"))) {
				t.Fatal("cancellation rewrote the original request")
			}
			streams := receipt["streams"].(map[string]any)
			for _, name := range []string{"stdout.log", "stderr.log"} {
				body := readBytes(t, filepath.Join(directory, name))
				proof := streams[name].(map[string]any)
				sum := sha256.Sum256(body)
				if proof["sha256"] != hex.EncodeToString(sum[:]) || proof["size"] != float64(len(body)) {
					t.Fatalf("original %s bytes differ from their receipt: %v", name, proof)
				}
				if when == "output" {
					expected := []byte("original diagnostic")
					if name == "stdout.log" {
						expected = bytes.Repeat([]byte("x"), 8<<20)
					}
					if !bytes.Equal(body, expected) {
						t.Fatalf("cancellation changed original %s bytes", name)
					}
				}
			}
		})
	}
}
