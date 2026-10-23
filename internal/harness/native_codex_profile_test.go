package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

// codexProfilePeer reports configuration from the actual start/resume parameters
// and rejects turn/start. It never executes a model or a workspace operation.
func codexProfilePeer(path string) {
	log, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var message map[string]any
		if err := decoder.Decode(&message); err == io.EOF {
			return
		} else if err != nil {
			panic(err)
		}
		if err := json.NewEncoder(log).Encode(message); err != nil {
			panic(err)
		}
		if message["id"] == nil {
			continue
		}
		response := map[string]any{"id": message["id"], "result": map[string]any{}}
		switch message["method"] {
		case "initialize":
		case "thread/start", "thread/resume":
			params := message["params"]
			profile, sandbox := "native-default", "readOnly"
			if permissions, ok := getOf(params, "permissions").(string); ok {
				if getOf(params, "sandbox") != nil {
					panic("permissions and sandbox must be exclusive")
				}
				profile, sandbox = permissions, "workspaceWrite"
			}
			if getOf(params, "sandbox") == "read-only" {
				profile = "native-readonly"
			}
			thread := "profile-thread"
			if message["method"] == "thread/resume" {
				thread = textOf(getOf(params, "threadId"))
			}
			configuration := map[string]any{"thread": map[string]any{"id": thread, "ephemeral": false}, "cwd": getOf(params, "cwd"), "model": getOf(params, "model"), "sandbox": map[string]any{"type": sandbox}, "approvalPolicy": "on-request", "approvalsReviewer": "user"}
			if os.Getenv("FORMAL_GATES_CODEX_PROFILE_ACK") != "missing" {
				if os.Getenv("FORMAL_GATES_CODEX_PROFILE_ACK") == "different" {
					profile = "another-profile"
				}
				configuration["activePermissionProfile"] = map[string]any{"id": profile, "extends": nil}
			}
			response["result"] = configuration
		case "turn/start":
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": "profile peer stops before model execution"}
		default:
			panic(fmt.Sprintf("unexpected method: %v", message["method"]))
		}
		if err := encoder.Encode(response); err != nil {
			panic(err)
		}
	}
}

func TestCodexPermissionProfileUsesIssuedBinding(t *testing.T) {
	for _, test := range []struct {
		name, profile, ack string
		readOnly, resume   bool
	}{
		{name: "workspace", profile: ":workspace"},
		{name: "named-profile", profile: "project-permissions"},
		{name: "resume", profile: ":workspace", resume: true},
		{name: "native-default"},
		{name: "readonly", profile: ":workspace", readOnly: true},
		{name: "readonly-resume", profile: "project-permissions", readOnly: true, resume: true},
		{name: "missing-profile", profile: ":workspace", ack: "missing"},
		{name: "different-profile", profile: ":workspace", ack: "different", resume: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := resolveNativePath(t.TempDir())
			directory := filepath.Join(root, "job")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEX_PERMISSION_PROFILE", test.profile)
			binding, err := NativeBinding("codex", settings.Values{Native: map[string]string{"codexExecutable": executable, "codexHome": root, "codexConfigRoot": root}})
			if err != nil {
				t.Fatal(err)
			}
			environment := objectOrEmpty(binding["environment"])
			if got, present := environment["CODEX_PERMISSION_PROFILE"]; (test.profile != "" && got != test.profile) || (test.profile == "" && present) {
				t.Fatalf("captured profile = %v (present %v), want %q", got, present, test.profile)
			}
			log := filepath.Join(directory, "peer.jsonl")
			environment["FORMAL_GATES_CODEX_PROFILE_PEER"] = log
			environment["FORMAL_GATES_CODEX_PROFILE_ACK"] = test.ack
			request := map[string]any{"provider": "codex", "binding": binding, "workspace": root, "prompt": "Do not execute a model", "read_only": test.readOnly, "model": "profile-model", "schema": map[string]any{"type": "object"}}
			if test.resume {
				request["identity"] = "existing-thread"
			}
			if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			original := readTestFileBytes(t, filepath.Join(directory, "native-request.json"))
			t.Setenv("CODEX_PERMISSION_PROFILE", "ambient-profile-after-capture")
			_, executeErr := codexExecute(request, directory, false)
			badProfile := test.ack != ""
			wantError := "profile peer stops before model execution"
			if badProfile {
				wantError = "native permission profile differs or is missing"
			}
			if executeErr == nil || !strings.Contains(executeErr.Error(), wantError) {
				t.Fatalf("execute error = %v, want %s", executeErr, wantError)
			}
			var threadRequest map[string]any
			turns := 0
			decoder := json.NewDecoder(bytes.NewReader(readTestFileBytes(t, log)))
			for decoder.More() {
				var message map[string]any
				if err := decoder.Decode(&message); err != nil {
					t.Fatal(err)
				}
				method := textOf(message["method"])
				if strings.HasPrefix(method, "thread/") {
					want := "thread/start"
					if test.resume {
						want = "thread/resume"
					}
					if method != want {
						t.Fatalf("thread method = %s, want %s", method, want)
					}
					threadRequest = objectOrEmpty(message["params"])
				}
				if method == "turn/start" {
					turns++
				}
			}
			wantPermissions := any(nil)
			if !test.readOnly && test.profile != "" {
				wantPermissions = test.profile
			}
			if threadRequest == nil || !valuesEqual(threadRequest["permissions"], wantPermissions) || (test.readOnly && threadRequest["sandbox"] != "read-only") || (!test.readOnly && threadRequest["sandbox"] != nil) {
				t.Fatalf("issued thread permissions = %v, profile %q readonly %v", threadRequest, test.profile, test.readOnly)
			}
			if (badProfile && turns != 0) || (!badProfile && turns != 1) {
				t.Fatalf("turn requests = %d, invalid profile %v", turns, badProfile)
			}
			for i := 0; i < 2; i++ {
				t.Setenv("CODEX_PERMISSION_PROFILE", fmt.Sprintf("ambient-recovery-%d", i))
				identity, err := NativeStartEvidence("codex", directory)
				if (badProfile && (err == nil || !strings.Contains(err.Error(), wantError))) || (!badProfile && (err != nil || identity == "")) {
					t.Fatalf("recovered start = %q, %v", identity, err)
				}
			}
			if !bytes.Equal(original, readTestFileBytes(t, filepath.Join(directory, "native-request.json"))) {
				t.Fatal("execution or start recovery changed the original issued binding")
			}
		})
	}
}
