package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

func panelHTTPServer(t *testing.T, b Backend) (*httptest.Server, string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, token := listener.Addr().String(), "panel-token"
	server := httptest.NewUnstartedServer(NewHandler(b, host, token))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server, host, token
}

func panelPost(t *testing.T, server *httptest.Server, host, token, path, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://"+host)
	request.Header.Set("X-Formal-Gates-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestPanelHTTPRejectsWrongHostOriginAndToken(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	server, host, token := panelHTTPServer(t, b)

	request, _ := http.NewRequest("GET", server.URL+"/api/state", nil)
	request.Host = "elsewhere.test"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host read = %d", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || state.Active != "默认" {
		t.Fatalf("state = %d %+v", response.StatusCode, state)
	}

	body := `{"operation":"use","name":"默认","expected":{"user":"","project":""}}`
	request, _ = http.NewRequest("POST", server.URL+"/api/configurations", strings.NewReader(body))
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing origin/token = %d", response.StatusCode)
	}

	request, _ = http.NewRequest("POST", server.URL+"/api/configurations", strings.NewReader(body))
	request.Header.Set("Origin", "http://attacker.example")
	request.Header.Set("X-Formal-Gates-Token", token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site origin = %d", response.StatusCode)
	}

	request, _ = http.NewRequest("POST", server.URL+"/api/configurations", strings.NewReader(body))
	request.Host = "elsewhere.test"
	request.Header.Set("Origin", "http://"+host)
	request.Header.Set("X-Formal-Gates-Token", token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host write = %d", response.StatusCode)
	}

	request, _ = http.NewRequest("POST", server.URL+"/api/configurations", strings.NewReader(body))
	request.Header.Set("Origin", "http://"+host)
	request.Header.Set("X-Formal-Gates-Token", "wrong")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token = %d", response.StatusCode)
	}

	response = panelPost(t, server, host, token, "/api/configurations", body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid write = %d", response.StatusCode)
	}
	effective, err := settings.Load(root, home)
	if err != nil || effective.ConfigurationName != "默认" {
		t.Fatalf("use did not write: %v", err)
	}
}

func TestPanelHTTPMutationsRequireBothRevisionsAndConflictWritesNothing(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	putJSON(t, projectConfigPath(root), `{"configurations":{"甲":{"qaModes":["blackbox"]}}}`)
	b := Backend{Root: root, Home: home}
	server, host, token := panelHTTPServer(t, b)
	beforeUser, beforeProject := overlayBytes(t, root, home)
	revisions := panelRevisions(t, b)

	save := map[string]any{
		"operation": "save", "scope": "project", "name": "新配置",
		"candidate": map[string]any{"qaModes": []string{"whitebox"}},
		"expected":  revisions,
	}
	body, _ := json.Marshal(save)
	post := func(payload map[string]any) *http.Response {
		data, _ := json.Marshal(payload)
		return panelPost(t, server, host, token, "/api/configurations", string(data))
	}

	// Missing the project revision is rejected before any write.
	missing := map[string]any{"operation": "save", "scope": "project", "name": "新配置", "candidate": save["candidate"], "expected": map[string]string{"user": revisions["user"]}}
	response := post(missing)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing project revision = %d", response.StatusCode)
	}

	// A stale user revision conflicts with zero writes.
	stale := map[string]any{"operation": "save", "scope": "project", "name": "新配置", "candidate": save["candidate"], "expected": map[string]string{"user": "stale", "project": revisions["project"]}}
	response = post(stale)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale revision = %d", response.StatusCode)
	}
	afterUser, afterProject := overlayBytes(t, root, home)
	if string(beforeUser) != string(afterUser) || string(beforeProject) != string(afterProject) {
		t.Fatal("rejected writes touched overlay bytes")
	}

	response = panelPost(t, server, host, token, "/api/configurations", string(body))
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid save = %d", response.StatusCode)
	}
	_, options, err := settings.LoadConfigurations(root, home)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, option := range options {
		found = found || option.Name == "新配置"
	}
	if !found {
		t.Fatal("save did not persist")
	}

	// Strict decoding refuses unknown fields before touching state.
	response = panelPost(t, server, host, token, "/api/configurations", `{"operation":"use","name":"默认","bogus":1,"expected":{"user":"","project":""}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", response.StatusCode)
	}

	// Non-panel settings are refused through the settings endpoint.
	response = panelPost(t, server, host, token, "/api/settings", `{"name":"capacity","value":"9","expected":{"user":"","project":""}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-panel setting = %d", response.StatusCode)
	}
	response = panelPost(t, server, host, token, "/api/settings", `{"name":"panelIdleTimeout","value":"600","expected":{"user":"","project":""}}`)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale setting revision = %d", response.StatusCode)
	}
	response = panelPost(t, server, host, token, "/api/settings", `{"name":"panelIdleTimeout","value":"600","expected":`+mustJSON(t, panelRevisions(t, b))+`}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("panel setting = %d", response.StatusCode)
	}

	// A missing runtime reports unavailable on the options endpoint without
	// breaking the library state.
	response, err = http.Get(server.URL + "/api/gates")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("gates without runtime = %d", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("state after failed options = %d", response.StatusCode)
	}
}

// TestOpenWindowErrorText: a missing managed browser appends the non-panel
// settings path to the window-open failure; other launch failures keep their
// own text.
func TestOpenWindowErrorText(t *testing.T) {
	var unavailable error = &ErrBrowserUnavailable{Reason: "no desktop session"}
	for _, err := range []error{unavailable, fmt.Errorf("launch: %w", unavailable)} {
		text := openWindowErrorText(err)
		for _, want := range []string{"managed browser unavailable", "settings set", "--execution-node", "only when the platform reports"} {
			if !strings.Contains(text, want) {
				t.Fatalf("browser-unavailable text for %v missing %q: %s", err, want, text)
			}
		}
	}
	other := errors.New("profile directory locked")
	if got := openWindowErrorText(other); got != other.Error() {
		t.Fatalf("ordinary launch error rewritten: %q", got)
	}
}

func TestPanelHTTPRejectsTrailingJSONDocument(t *testing.T) {
	home := panelHome(t)
	root := t.TempDir()
	b := Backend{Root: root, Home: home}
	server, host, token := panelHTTPServer(t, b)
	beforeUser, beforeProject := overlayBytes(t, root, home)

	body := `{"operation":"use","name":"默认","expected":{"user":"","project":""}} {"operation":"use","name":"甲"}`
	response := panelPost(t, server, host, token, "/api/configurations", body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing document = %d", response.StatusCode)
	}
	afterUser, afterProject := overlayBytes(t, root, home)
	if string(beforeUser) != string(afterUser) || string(beforeProject) != string(afterProject) {
		t.Fatal("a rejected body touched overlay bytes")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
