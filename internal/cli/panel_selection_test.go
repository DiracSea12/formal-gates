package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/panel"
	"formal-gates/internal/settings"
)

type observedPanelTransport struct {
	base    http.RoundTripper
	address string
	opened  chan struct{}
	once    sync.Once
}

func (t *observedPanelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && request.URL.Host == t.address && request.URL.Path == "/api/windows/open" && response.StatusCode == http.StatusOK {
		t.once.Do(func() { close(t.opened) })
	}
	return response, err
}
func waitSelectionOutput(t *testing.T, out *lockedTestBuffer, token string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), token) {
		if time.Now().After(deadline) {
			t.Fatalf("missing %q: %s", token, out.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConversationSelectsPendingPanelAndStartsConfirmedIntake(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, form := range []string{"name", "position", "default"} {
		t.Run(form, func(t *testing.T) { exercisePendingPanelSelection(t, true, form) })
	}
}

func TestPanelClickResolvesConfigureBeforeServiceStops(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	exercisePendingPanelSelection(t, false, "name")
}

func exercisePendingPanelSelection(t *testing.T, conversation bool, form string) {
	t.Helper()
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	saveReviewOnlyConfiguration(t, root)
	cliStartRepo(t, root)
	preview, err := facade.PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := panel.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate, position, err := facade.SaveOriginalPayload(workflowPayloadDirectory(canonical, "", "intake"), mustJSON(t, preview.IntakeCandidate))
	if err != nil {
		t.Fatal(err)
	}
	before, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Configurations []settings.ConfigurationOption `json:"configurations"`
	}
	if err := json.Unmarshal(settingsCommand(t, root, home, "show"), &shown); err != nil {
		t.Fatal(err)
	}
	var chosen settings.ConfigurationOption
	for _, option := range shown.Configurations {
		if option.Active {
			chosen = option
			break
		}
	}
	if chosen.Name == "" {
		t.Fatal("no active configuration was published")
	}
	selection := chosen.Name
	switch form {
	case "position":
		selection = strconv.Itoa(chosen.Position)
	case "default":
		selection = ""
	}
	expectedConfiguration := chosen.Name

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := panel.PrepareService(ctx, panel.Backend{Root: root, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	serviceStopped := make(chan struct{})
	var serviceErr error
	go func() { defer close(serviceStopped); serviceErr = service.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-serviceStopped; service.Release() })
	window, err := service.Server.NewWindow(candidate)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	observer := &observedPanelTransport{base: original, address: service.Instance.Addr, opened: make(chan struct{})}
	http.DefaultTransport = observer
	defer func() { http.DefaultTransport = original }()
	in, feed := io.Pipe()
	defer in.Close()
	defer feed.Close()
	var out, stderr lockedTestBuffer
	done := make(chan error, 1)
	go func() {
		code, err := runWorkflowServe([]string{"--root", root}, IO{Context: ctx, Stdin: in, Stdout: &out, Stderr: &stderr})
		if err == nil && code != 0 {
			err = fmt.Errorf("serve exit %d", code)
		}
		done <- err
	}()
	encoder := json.NewEncoder(feed)
	send := func(id int, name string, args any) {
		t.Helper()
		if err := encoder.Encode(map[string]any{"id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		t.Fatal(err)
	}
	send(2, "configure", map[string]any{"intake": position})
	select {
	case <-observer.opened:
	case <-time.After(10 * time.Second):
		t.Fatalf("configure did not open existing panel: %s %s", out.String(), stderr.String())
	}
	select {
	case <-window.Done:
		t.Fatal("opening panel completed selection")
	default:
	}
	if conversation {
		send(3, "confirm_start", map[string]any{"intake": position, "configuration": selection})
		waitSelectionOutput(t, &out, `"id":3`)
	} else {
		body := fmt.Sprintf(`{"window":%q,"configuration":%q}`, window.ID, expectedConfiguration)
		request, err := http.NewRequestWithContext(ctx, "POST", "http://"+service.Instance.Addr+"/api/window/select", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://"+service.Instance.Addr)
		request.Header.Set("X-Formal-Gates-Token", service.Instance.Token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("panel click: %d", response.StatusCode)
		}
		waitSelectionOutput(t, &out, `selected`)
		selectedStates, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
		if err != nil || !reflect.DeepEqual(before, selectedStates) {
			t.Fatal("configuration selection alone started a run", selectedStates, err)
		}
		send(3, "confirm_start", map[string]any{"intake": position, "configuration": selection})
		waitSelectionOutput(t, &out, `"id":3`)
	}
	select {
	case <-window.Done:
	default:
		t.Fatal("startup did not resolve the pending configuration panel")
	}
	result := window.Result()
	if result.Status != "selected" || result.Configuration != expectedConfiguration {
		t.Fatalf("panel choice: %+v", result)
	}
	open, _, err := service.WindowStatus(ctx, window.ID)
	if err != nil {
		// The service stops once its last window closes, so this status read
		// can lose to that shutdown; only a stopped service with no shutdown
		// error proves the window closed, any other status failure still fails.
		select {
		case <-serviceStopped:
			if serviceErr != nil {
				t.Fatalf("last-window service shutdown: %v", serviceErr)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("conversation left its panel open: %v %v", open, err)
		}
	} else if open {
		t.Fatalf("conversation left its panel open: %v %v", open, err)
	}
	waitSelectionOutput(t, &out, `selected`)
	waitSelectionOutput(t, &out, `"id":3`)
	feed.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v %s %s", err, out.String(), stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not finish")
	}
	after, err := filepath.Glob(filepath.Join(root, facade.EngineNamespace, "*", "state.json"))
	if err != nil || len(after) != len(before)+1 {
		t.Fatalf("confirmed startup did not create exactly one run: before=%v after=%v err=%v", before, after, err)
	}
	select {
	case <-serviceStopped:
		if serviceErr != nil {
			t.Fatalf("last-window service shutdown: %v", serviceErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("last-window service did not stop after delivering selected")
	}
	decoder := json.NewDecoder(strings.NewReader(out.String()))
	var started facade.Run
	for decoder.More() {
		var frame struct {
			ID     int `json:"id"`
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.ID == 3 {
			if frame.Result.IsError || len(frame.Result.Content) != 1 {
				t.Fatal("confirmed startup returned an error", frame)
			}
			if err := json.Unmarshal([]byte(frame.Result.Content[0].Text), &started); err != nil {
				t.Fatal(err)
			}
		}
	}
	var request facade.StartRequest
	if err := readJSONFileInto(filepath.Join(root, facade.EngineNamespace, started.RunID, "request.json"), &request); err != nil {
		t.Fatal(err)
	}
	if started.Status != "ACTIVE" || request.IntakeConfirmationReceipt.Confirmation != "conversation" || request.IntakeConfirmationReceipt.Digest != preview.IntakeCandidate.Digest || strings.Contains(out.String(), `"method":"elicitation/create"`) {
		t.Fatal("confirmed startup did not preserve its source without a form", started.RunID, started.Status)
	}
}
