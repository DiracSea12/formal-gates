package persistence

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTerminalCleanupRetiresRunPayloadsThroughRecovery(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	files := map[string][]byte{
		filepath.Join(dir, "state.json"):                               []byte("active projection"),
		filepath.Join(dir, "payloads", "control", "payload-000000001"): []byte("original confirmation\r\n原文  "),
		filepath.Join(dir, "request.json"):                             []byte(`{"runId":"run"}`),
		filepath.Join(dir, "terminal-summary.json"):                    []byte(`{"runId":"run","status":"ABORTED"}`),
		filepath.Join(dir, "native", "source.json"):                    []byte(`{"sessionId":"original-session"}`),
		filepath.Join(root, "payloads", "project-file"):                []byte("unrelated project bytes"),
		filepath.Join(root, "other-run", "payloads", "input"):          []byte("other run bytes"),
	}
	write := func(path string, body []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range files {
		write(path, body)
	}
	store, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatal(err)
	}
	// The state-removal prefix reopens the same durable cleanup intent.
	write(store.cleanupIntentPath(), []byte("{\"status\":\"requested\"}\n"))
	if err := os.Remove(store.statePath()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- store.CleanupTerminal() }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := os.ReadFile(store.cleanupReceiptPath())
	if err != nil || string(receipt) != "{\"entries\":[],\"residue\":false,\"status\":\"reconciled\"}\n" {
		t.Fatal("cleanup did not physically reconcile its owned payload", string(receipt), err)
	}
	for _, path := range []string{store.statePath(), filepath.Join(dir, "payloads"), store.cleanupIntentPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("owned cleanup resource remains", path, err)
		}
	}
	reopened, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := os.Stat(store.cleanupReceiptPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.CleanupTerminal(); err != nil {
		t.Fatal(err)
	}
	again, err := os.Stat(store.cleanupReceiptPath())
	if err != nil || !again.ModTime().Equal(stamp.ModTime()) {
		t.Fatal("completed replay refreshed immutable cleanup evidence", err)
	}
	for path, expected := range files {
		if path == store.statePath() || strings.HasPrefix(path, filepath.Join(dir, "payloads")+string(filepath.Separator)) {
			continue
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("cleanup changed retained or unrelated bytes", path, err)
		}
	}
	// A recovered unfinished prefix may still carry the original payload bytes.
	write(filepath.Join(dir, "payloads", "control", "payload-000000001"), files[filepath.Join(dir, "payloads", "control", "payload-000000001")])
	write(store.cleanupIntentPath(), []byte("{\"status\":\"requested\"}\n"))
	if err := reopened.CleanupTerminal(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(store.cleanupReceiptPath())
	if err != nil || !bytes.Equal(after, receipt) {
		t.Fatal("recovery changed reconciled receipt bytes", err)
	}
	unknown := filepath.Join(dir, "unknown-resource")
	write(unknown, []byte("retain unknown bytes"))
	if err := reopened.CleanupTerminal(); err == nil || !strings.Contains(err.Error(), "residue remains: unknown-resource") {
		t.Fatal("owned payload cleanup bypassed unknown residue verification", err)
	}
	if actual, err := os.ReadFile(unknown); err != nil || string(actual) != "retain unknown bytes" {
		t.Fatal("unknown resource was removed", err)
	}
}
