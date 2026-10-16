package facade

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestApprovedCommandsUseRegisteredWorkspaceEnvironment(t *testing.T) {
	root := t.TempDir()
	peer := filepath.Join(t.TempDir(), "workspace-peer")
	if os.PathSeparator == '\\' {
		peer += ".exe"
	}
	build := exec.Command("go", "build", "-o", peer, filepath.Join("testdata", "workspace_command_peer.go"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build workspace peer: %v %s", err, output)
	}
	t.Setenv("FORMAL_GATES_WORKSPACE", filepath.Join(root, "ambient workspace"))
	command := `"` + peer + `"`
	state := &protocol.State{RunID: "run", NativeResources: map[string]protocol.NativeResource{}}
	facade := &Facade{}
	for _, identity := range []string{"current", "other"} {
		workspace := filepath.Join(root, identity+" workspace")
		subdirectory := filepath.Join(workspace, "relative directory")
		if err := os.MkdirAll(subdirectory, 0755); err != nil {
			t.Fatal(err)
		}
		for path, content := range map[string]string{filepath.Join(workspace, "identity"): identity + " root\n", filepath.Join(subdirectory, "identity"): identity + " child\n"} {
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		state.NativeResources[identity] = protocol.NativeResource{ID: identity, Path: workspace, Owner: state.RunID, Status: "READY"}
	}
	for _, identity := range []string{"current", "other"} {
		workspace := state.NativeResources[identity].Path
		for _, kind := range []string{"development", "qa-shell", "qa-argv"} {
			parameters := protocol.CheckParameters{WorkspaceID: identity, Check: protocol.DevelopmentCheck{Command: command}}
			expectedArgv := coverage.ShellCommand(command)
			expectedStdin := ""
			expectedCwd := workspace
			if kind != "development" {
				parameters.QA = &protocol.QACheckParameters{WorkspaceRoot: workspace, Directory: "relative directory", Operation: coverage.Operation{Kind: coverage.OperationShell, Command: command, Stdin: "stdin value\n"}}
				expectedStdin, expectedCwd = parameters.QA.Operation.Stdin, filepath.Join(workspace, parameters.QA.Directory)
				if kind == "qa-argv" {
					parameters.QA.Operation = coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{peer, "argument identity", ""}, Stdin: expectedStdin}
					expectedArgv = parameters.QA.Operation.Argv
				}
			}
			request, err := facade.checkRequest(state, protocol.HostActionIntent{PayloadDigest: "approved inputs"}, parameters)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(request["argv"], expectedArgv) || request["stdin"] != expectedStdin || request["cwd"] != expectedCwd {
				t.Fatal("approved argv, stdin or relative directory changed")
			}
			fresh, err := facade.checkRequest(state, protocol.HostActionIntent{PayloadDigest: "approved inputs"}, parameters)
			if err != nil || !reflect.DeepEqual(request, fresh) {
				t.Fatal("observation did not derive the same approved request")
			}
			invoke := func(binding string) string {
				argv := request["argv"].([]string)
				child := exec.Command(argv[0], argv[1:]...)
				child.Dir = request["cwd"].(string)
				child.Stdin = strings.NewReader(request["stdin"].(string))
				child.Env = append(os.Environ(), "FORMAL_GATES_WORKSPACE="+binding)
				out, err := child.CombinedOutput()
				if err != nil {
					t.Fatalf("approved child failed: %v %s", err, out)
				}
				return string(out)
			}
			expected := identity + " root\n" + identity + " child\n" + expectedStdin
			if kind == "qa-argv" {
				expected += "<>"
			}
			binding := request["environment"].(map[string]any)["FORMAL_GATES_WORKSPACE"].(string)
			if actual := invoke(binding); actual != expected {
				t.Fatalf("%s %s observed %q, want %q", identity, kind, actual, expected)
			}
			other := "other"
			if identity == other {
				other = "current"
			}
			if wrong := invoke(state.NativeResources[other].Path); wrong == expected {
				t.Fatal("different input workspace produced the same evidence")
			}
		}
	}
	literal := `cd '/old approved workspace' && printf '%s' "unchanged"`
	parameters := protocol.CheckParameters{WorkspaceID: "current", QA: &protocol.QACheckParameters{WorkspaceRoot: state.NativeResources["current"].Path, Operation: coverage.Operation{Kind: coverage.OperationShell, Command: literal}}}
	request, err := facade.checkRequest(state, protocol.HostActionIntent{}, parameters)
	if err != nil || !reflect.DeepEqual(request["argv"], coverage.ShellCommand(literal)) {
		t.Fatal("original fixed-path shell was silently rewritten")
	}
}
