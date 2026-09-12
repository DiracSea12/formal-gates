package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkBuddyCarrierWorkspaceKeepsProjectKeyShortAndDeterministic(t *testing.T) {
	workspace := t.TempDir()
	identity := "11111111-2222-4333-8444-555555555555"
	carrier, cleanup, err := workBuddyCarrierWorkspace(identity, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(carrier) || carrier == workspace || !strings.HasPrefix(carrier, filepath.Clean(os.TempDir())+string(os.PathSeparator)) {
		t.Fatalf("carrier=%q workspace=%q tmp=%q", carrier, workspace, os.TempDir())
	}
	// WorkBuddy flattens the canonical cwd into one project-directory component.
	// The carrier must therefore keep that complete flattened value below the
	// common 255-byte component limit even when the actual workspace is deep.
	canonical, err := filepath.EvalSymlinks(carrier)
	if err != nil {
		t.Fatal(err)
	}
	compressed := strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(canonical), "-")
	for strings.Contains(compressed, "--") {
		compressed = strings.ReplaceAll(compressed, "--", "-")
	}
	if len(compressed) >= 200 {
		t.Fatalf("carrier would still create an unsafe WorkBuddy project component: %d %q", len(compressed), compressed)
	}
	if err := os.WriteFile(filepath.Join(carrier, "leak"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(carrier); !os.IsNotExist(err) {
		t.Fatalf("carrier cleanup failed: %v", err)
	}
	carrier2, cleanup2, err := workBuddyCarrierWorkspace(identity, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if carrier2 != carrier {
		t.Fatalf("resume changed WorkBuddy project key: %q != %q", carrier2, carrier)
	}
}

func TestWorkBuddyAuthorizedWorkspaceUsesAddDirWithoutChangingTextOnlyRoles(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "authorized")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	schema := json.RawMessage(`{"type":"object"}`)
	for _, tc := range []struct {
		step       string
		wantAddDir bool
	}{
		{step: "development-worker", wantAddDir: true},
		{step: "blackbox-design", wantAddDir: true},
		{step: "product-review", wantAddDir: false},
		{step: "start-readiness", wantAddDir: false},
	} {
		t.Run(tc.step, func(t *testing.T) {
			cmd, err := workBuddyCommand("/bin/workbuddy", t.TempDir(), "", "", "", schema, filepath.Join(t.TempDir(), "mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			workBuddyRoleTools(cmd, tc.step)
			before := append([]string(nil), cmd.Args...)
			prompt, err := workBuddyAuthorizeWorkspace(cmd, tc.step, workspace, "bound prompt")
			if err != nil {
				t.Fatal(err)
			}
			hasAdd := false
			for i := 0; i+1 < len(cmd.Args); i++ {
				if cmd.Args[i] == "--add-dir" && cmd.Args[i+1] == workspace {
					hasAdd = true
				}
			}
			if hasAdd != tc.wantAddDir {
				t.Fatalf("args=%q", cmd.Args)
			}
			if tc.wantAddDir {
				if !strings.Contains(prompt, workspace) || !strings.Contains(prompt, "transport directory") {
					t.Fatalf("workspace mapping missing from prompt: %q", prompt)
				}
			} else if prompt != "bound prompt" || len(cmd.Args) != len(before) {
				t.Fatalf("text-only role gained workspace access: args=%q prompt=%q", cmd.Args, prompt)
			}
		})
	}
}
