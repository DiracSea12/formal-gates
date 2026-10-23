package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The panel service child is this test binary re-executed; init() catches the
// env marker before the testing framework consumes the config arguments.
func init() {
	if os.Getenv("FORMAL_GATES_CLI_HELPER") != "1" || len(os.Args) < 2 || os.Args[1] != "config" {
		return
	}
	code, err := runConfig(os.Args[2:], IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if code == 0 && err != nil {
		code = 1
	}
	os.Exit(code)
}

// TestConfigServeOwnedStopsOnStdinEOF: an owned spawned service exits when its
// owner pipe closes, taking its instance record and listener with it.
func TestConfigServeOwnedStopsOnStdinEOF(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	stdin, owner := io.Pipe()
	var stdout, stderr lockedTestBuffer
	code := make(chan int, 1)
	go func() {
		c, err := runConfig([]string{"--serve", "--owned", "--root", root},
			IO{Stdin: stdin, Stdout: &stdout, Stderr: &stderr})
		if err != nil {
			t.Errorf("serve: %v", err)
			c = 1
		}
		code <- c
	}()
	var descriptor struct {
		Ready     bool   `json:"ready"`
		Delegated bool   `json:"delegated"`
		Addr      string `json:"addr"`
		Token     string `json:"token"`
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if json.Unmarshal(stdout.Bytes(), &descriptor) == nil && descriptor.Addr != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if descriptor.Addr == "" || descriptor.Token == "" || descriptor.Delegated {
		t.Fatalf("owned service printed no descriptor: %q stderr=%q", stdout.String(), stderr.String())
	}
	response, err := http.Get("http://" + descriptor.Addr + "/api/state")
	if err != nil {
		t.Fatalf("owned service not serving: %v", err)
	}
	response.Body.Close()

	// A second serve invocation delegates: same descriptor, exits on its own.
	// Its stdin is an open owner pipe like the MCP host keeps, not EOF.
	delegateIn, delegateOwner := io.Pipe()
	defer delegateOwner.Close()
	var delegated, delegateErr lockedTestBuffer
	c, err := runConfig([]string{"--serve", "--owned", "--root", root},
		IO{Stdin: delegateIn, Stdout: &delegated, Stderr: &delegateErr})
	if err != nil {
		t.Fatalf("delegate serve: %v", err)
	}
	if c != 0 {
		t.Fatalf("delegate serve exited %d: %s", c, delegateErr.String())
	}
	var second struct {
		Delegated bool   `json:"delegated"`
		Addr      string `json:"addr"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(delegated.Bytes(), &second); err != nil || !second.Delegated {
		t.Fatalf("second serve did not delegate: %v %q", err, delegated.String())
	}
	if second.Addr != descriptor.Addr || second.Token != descriptor.Token {
		t.Fatal("delegated descriptor differs from the live service")
	}

	// Owner EOF ends the service and removes the instance record.
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-code:
		if exit != 0 {
			t.Fatalf("owned serve exit code %d", exit)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("owner EOF did not stop the service")
	}
	if _, err := os.Stat(root + string(os.PathSeparator) + ".gates" + string(os.PathSeparator) + "panel-instance.json"); !os.IsNotExist(err) {
		t.Fatalf("instance record survived owner EOF: %v", err)
	}
	if response, err := http.Get("http://" + descriptor.Addr + "/api/state"); err == nil {
		response.Body.Close()
		t.Fatal("listener survived owner EOF")
	}
}

// TestControllerReportsDisabledSelection: with panelSelection off and no
// force flag the MCP configure call answers disabled without touching the
// service.
func TestControllerReportsDisabledSelection(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"panelSelection":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	controller, err := newPanelController()
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	result, err := controller.configure(context.Background(), root, 0, false)
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if result.Status != "disabled" {
		t.Fatalf("panelSelection=false configure = %q, want disabled", result.Status)
	}
}

// TestConfigCommandIgnoresPanelSelection: running `formal-gates config` is the
// explicit request, so a disabled panelSelection must not refuse it. A
// canceled caller context stops the service setup; the asserted invariant is
// that the failure is not the disabled reply.
func TestConfigCommandIgnoresPanelSelection(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(root, ".gates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"panelSelection":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr lockedTestBuffer
	code, err := runConfig([]string{"--root", root},
		IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Context: ctx})
	if code == 0 || err == nil {
		t.Fatalf("canceled config run returned %d nil", code)
	}
	if strings.Contains(err.Error(), "disabled") || strings.Contains(err.Error(), "panelSelection") {
		t.Fatalf("config refused on panelSelection: %v", err)
	}
}

// TestWorkflowServeListsConfigureTool: the installed MCP seam discovers
// configure beside the three unchanged confirmation tools, with a closed
// additionalProperties schema carrying exactly root/intake/force.
func TestWorkflowServeListsConfigureTool(t *testing.T) {
	wire := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	var out, stderr lockedTestBuffer
	code, err := runWorkflowServe(nil, IO{Stdin: strings.NewReader(wire), Stdout: &out, Stderr: &stderr})
	if err != nil || code != 0 {
		t.Fatalf("serve: code=%d err=%v stderr=%s", code, err, stderr.String())
	}
	type toolDecl struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             json.RawMessage            `json:"required"`
			AdditionalProperties bool                       `json:"additionalProperties"`
		} `json:"inputSchema"`
	}
	var declared []toolDecl
	decoder := json.NewDecoder(strings.NewReader(out.String()))
	for decoder.More() {
		var frame struct {
			ID     float64 `json:"id"`
			Result struct {
				Tools []toolDecl `json:"tools"`
			} `json:"result"`
		}
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.ID == 2 {
			declared = frame.Result.Tools
		}
	}
	names := map[string]toolDecl{}
	for _, toolFrame := range declared {
		names[toolFrame.Name] = toolFrame
		var required []string
		if len(toolFrame.InputSchema.Required) == 0 || string(toolFrame.InputSchema.Required) == "null" || json.Unmarshal(toolFrame.InputSchema.Required, &required) != nil {
			t.Errorf("%s required must encode a JSON array: %s", toolFrame.Name, toolFrame.InputSchema.Required)
		}
		for _, field := range required {
			if _, ok := toolFrame.InputSchema.Properties[field]; !ok {
				t.Errorf("%s requires undeclared field %s", toolFrame.Name, field)
			}
		}
	}
	for _, want := range []string{"confirm_start", "confirm_control", "confirm_native", "configure"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("tools/list missing %s: %v", want, names)
		}
	}
	configure := names["configure"]
	for _, guidance := range []string{"settings show", "execution-options", "panel and conversation are not exclusive", "while panel is open", "panelSelection controls default opening", "without disabling panelSelection", "resolves this configure waiter as selected", "confirm_start", "confirm_control"} {
		if !strings.Contains(configure.Description, guidance) {
			t.Errorf("configure guidance missing %q: %s", guidance, configure.Description)
		}
	}
	if configure.InputSchema.AdditionalProperties {
		t.Fatalf("configure schema is not closed: %+v", configure.InputSchema)
	}
	for _, field := range []string{"root", "intake", "force"} {
		if _, ok := configure.InputSchema.Properties[field]; !ok {
			t.Fatalf("configure schema missing %s: %v", field, configure.InputSchema.Properties)
		}
	}
}

type lockedTestBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *lockedTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *lockedTestBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf...)
}

func (b *lockedTestBuffer) String() string { return string(b.Bytes()) }

// processAlive answers whether pid names a live process without killing or
// attaching to it.
func processAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
		return err == nil && strings.Contains(string(out), fmt.Sprint(pid))
	}
	proc, err := os.FindProcess(pid)
	return err == nil && proc.Signal(syscall.Signal(0)) == nil
}

// TestControllerRespawnsAfterOwnedServiceExit: a cached service child that
// died on its own is evicted and the next configure path spawns a fresh one
// under the same still-live controller.
func TestControllerRespawnsAfterOwnedServiceExit(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORMAL_GATES_CLI_HELPER", "1")

	controller, err := newPanelController()
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	ctx := context.Background()

	first, firstSvc, err := controller.ensureService(ctx, root)
	if err != nil {
		t.Fatalf("first ensureService: %v", err)
	}
	if first.Addr == "" || first.Token == "" {
		t.Fatalf("first service descriptor incomplete: %+v", first)
	}
	if firstSvc == nil {
		t.Fatal("first ensureService delegated where no owner existed")
	}
	firstPID := firstSvc.proc.Cmd.Process.Pid

	// The child dies (idle timeout equivalent); the next ensure must not
	// reuse the dead process.
	_ = firstSvc.proc.Cmd.Process.Kill()
	select {
	case <-firstSvc.proc.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("dead child was never observed")
	}

	second, secondSvc, err := controller.ensureService(ctx, root)
	if err != nil {
		t.Fatalf("ensureService after child exit: %v", err)
	}
	if secondSvc == nil || secondSvc == firstSvc {
		t.Fatalf("dead service was not respawned: %+v", second)
	}
	secondPID := secondSvc.proc.Cmd.Process.Pid
	if secondPID == firstPID {
		t.Fatal("respawned service reused the dead PID")
	}
	// The fresh child answers its identity endpoint with its own record.
	request, err := http.NewRequest("GET", "http://"+second.Addr+"/api/instance", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = second.Addr
	request.Header.Set("X-Formal-Gates-Token", second.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("respawned service not serving: %v", err)
	}
	defer response.Body.Close()
	var identity struct {
		PID int `json:"pid"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&identity) != nil || identity.PID != secondPID {
		t.Fatalf("respawned service identity mismatch: status=%d pid=%d want=%d", response.StatusCode, identity.PID, secondPID)
	}
}

// TestWorkflowServeEOFReleasesOwnedPanel: stdin EOF on the MCP transport
// cancels the in-flight configure, ends the serve loop, and the deferred
// controller close tears down the owned service's lock, record, and listener.
func TestWorkflowServeEOFReleasesOwnedPanel(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORMAL_GATES_CLI_HELPER", "1")

	input, feed := io.Pipe()
	var out, errors lockedTestBuffer
	done := make(chan int, 1)
	go func() {
		code, err := runWorkflowServe(nil, IO{Stdin: input, Stdout: &out, Stderr: &errors})
		if err != nil {
			t.Errorf("workflow serve: %v", err)
			code = 1
		}
		done <- code
	}()
	fmt.Fprint(feed, `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`+"\n")
	fmt.Fprintf(feed, `{"id":2,"method":"tools/call","params":{"name":"configure","arguments":{"root":%q,"force":true}}}`+"\n", filepath.ToSlash(root))

	// The spawned service publishes its record; capture the child PID.
	record := filepath.Join(root, ".gates", "panel-instance.json")
	var instance struct {
		PID   int    `json:"pid"`
		Addr  string `json:"addr"`
		Token string `json:"token"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		body, err := os.ReadFile(record)
		if err == nil && json.Unmarshal(body, &instance) == nil && instance.Addr != "" {
			request, _ := http.NewRequest("GET", "http://"+instance.Addr+"/api/instance", nil)
			request.Host = instance.Addr
			request.Header.Set("X-Formal-Gates-Token", instance.Token)
			if response, err := http.DefaultClient.Do(request); err == nil && response.StatusCode == http.StatusOK {
				response.Body.Close()
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("panel service never became ready; stderr=%q stdout=%q", errors.String(), out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := feed.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("workflow serve exit %d: stderr=%q", code, errors.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("EOF did not stop the workflow serve loop")
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("instance record survived owner EOF: %v", err)
	}
	if response, err := http.Get("http://" + instance.Addr + "/api/instance"); err == nil {
		response.Body.Close()
		t.Fatal("listener survived owner EOF")
	}
	for waited := time.Duration(0); waited < 10*time.Second; waited += 100 * time.Millisecond {
		if !processAlive(instance.PID) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("owned service process %d survived owner EOF", instance.PID)
}

// An explicit intake of zero is an invalid position, not an omitted library
// open; the handler rejects it before the controller or any window exists.
func TestConfigureRejectsExplicitZeroIntake(t *testing.T) {
	if _, err := callConfirmationTool("configure", json.RawMessage(`{"intake":0}`), IO{}, nil, "", ""); err == nil || !strings.Contains(err.Error(), "select an original intake position") {
		t.Fatalf("explicit zero intake was not rejected first: %v", err)
	}
	if _, err := callConfirmationTool("configure", json.RawMessage(`{}`), IO{}, nil, "", ""); err == nil || !strings.Contains(err.Error(), "panel controller unavailable") {
		t.Fatalf("omitted intake did not reach the controller check: %v", err)
	}
}
