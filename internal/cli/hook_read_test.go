package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/encoder"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHookAllowsLegacyNameReading(t *testing.T) {
	for _, command := range []string{
		`rg -n 'gate-workflow.ps1' .`,
		`cat scripts/gate-state.ps1`,
		`git log -- hooks/capture-subagent-receipt.ps1`,
	} {
		for _, provider := range []string{"", "codex", "zcode"} {
			t.Run(provider+"/"+command, func(t *testing.T) {
				payload, err := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": command}})
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"hook", "decide"}
				if provider != "" {
					args = append(args, "--provider", provider)
				}
				var out, errOut bytes.Buffer
				code := Run("formal-gates", args, IO{Stdin: bytes.NewReader(payload), Stdout: &out, Stderr: &errOut})
				if code != 0 || errOut.Len() != 0 || strings.Contains(out.String(), `"block"`) || strings.Contains(out.String(), `"deny"`) {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
				}
				t.Logf("exit=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			})
		}
	}
}

// Exercise the public CLI with a disposable active development run. Commands
// are hook payload data; no shell writes or real runs are executed.
func TestRunHookWriteCommandSemantics(t *testing.T) {
	root := t.TempDir()
	outside := filepath.ToSlash(filepath.Join(t.TempDir(), "outside.go"))
	dir := filepath.Join(root, ".gates", "engine", "hook-test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"writer": "engine", "stateSchemaVersion": encoder.StateSchemaVersion,
		"content": map[string]any{
			"regular":           map[string]any{"terminal": "", "requirement": map[string]any{"path": "requirements.md"}},
			"regularExecutions": map[string]any{"dev": map[string]any{"claimed": true, "claim": map[string]any{"task": "entry/development-worker/B1"}}},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		command string
		deny    bool
	}{
		{`rg -n 'git commit' README.md`, false},
		{`git log --oneline --grep="git commit"`, false},
		{`rg -n 'svn commit|p4 edit|tee |sed -i|install |touch |mkdir |rm |mv |cp ' README.md`, false},
		{`rg -n "git commit; touch main.go && rm main.go" README.md`, false},
		{`rg -n "say \"git commit\"; cp main.go" README.md`, false},
		{`rg -n git\ commit README.md`, false},
		{`rg -n '|' README.md; git log --grep='git commit'`, false},
		{`git log --grep git -- README.md commit`, false},
		{`rg -n cp main.go`, false},
		{`git show HEAD:main.go`, false},
		{`sed -n 's/touch /cp /p' README.md`, false},
		{`npm install -g example`, false},
		{`command -v cp`, false},
		{`command -V git`, false},
		{`git log --grep="git commit" -- README.md`, false},
		{`git commit-tree --help`, false},
		{`git log --grep="git checkout --"`, false},
		{`rg -n 'git commit > main.go' README.md > /dev/null 2>&1`, false},
		{`git log --grep="git commit" 2>/dev/null`, false},
		{`git commit -m delivery`, true},
		{`git -C . commit -m delivery`, true},
		{`git -c user.name=Review commit -m delivery`, true},
		{`git reset --hard HEAD`, true},
		{`git checkout -- main.go`, true},
		{`svn commit -m delivery`, true},
		{`p4 edit main.go`, true},
		{`touch main.go`, true},
		{`mkdir .gates/example`, true},
		{`mv a.go main.go`, true},
		{`install a.go main.go`, true},
		{`rg -n cp /tmp/example.go > main.go`, true},
		{`/usr/bin/cp a.go main.go`, true},
		{`sudo cp a.go main.go`, true},
		{`env LANG=C command cp a.go main.go`, true},
		{`sed -i 's/a/b/' main.go`, true},
		{`sed -n 's/a/b/p' main.go`, false},
		{`git status&&git commit -m delivery`, true},
		{`git status;touch main.go`, true},
		{"git status\ntouch main.go", true},
		{`git status||rm main.go`, true},
		{`git status&touch main.go`, true},
		{`cat README.md|tee main.go`, true},
		{`touch notes.md; cp a.go main.go`, true},
		{`sh -c 'git status; touch main.go'`, true},
		{`sh -c "rg -n 'git commit' README.md"`, false},
		{`rg -n 'git commit' README.md > main.go`, true},
		{`git log --grep="git commit" >> main.go`, true},
		{`git status 2> main.go`, true},
		{`git status &> main.go`, true},
		{`git status >| main.go`, true},
		{fmt.Sprintf("touch %q 2>&1", outside), false},
		{fmt.Sprintf("touch %q &> /dev/null", outside), false},
		{fmt.Sprintf("touch %q >| /dev/null", outside), false},
		{fmt.Sprintf("touch %q; touch main.go", outside), true},
	} {
		for _, agent := range []string{"qa-review", ""} {
			for _, provider := range []string{"", "codex", "zcode"} {
				t.Run(agent+"/"+provider+"/"+tc.command, func(t *testing.T) {
					payload, err := json.Marshal(map[string]any{"cwd": root, "agent_type": agent, "tool_name": "Bash", "tool_input": map[string]string{"command": tc.command}})
					if err != nil {
						t.Fatal(err)
					}
					args := []string{"hook", "decide"}
					if provider != "" {
						args = append(args, "--provider", provider)
					}
					var out, errOut bytes.Buffer
					code := Run("formal-gates", args, IO{Stdin: bytes.NewReader(payload), Stdout: &out, Stderr: &errOut})
					wantCode := 0
					if tc.deny && provider != "codex" {
						wantCode = 2
					}
					denied := strings.Contains(out.String(), `"block"`) || strings.Contains(out.String(), `"deny"`)
					if code != wantCode || errOut.Len() != 0 || denied != tc.deny || (!tc.deny && provider != "" && out.Len() != 0) {
						t.Fatalf("want deny=%v exit=%d; exit=%d stdout=%q stderr=%q", tc.deny, wantCode, code, out.String(), errOut.String())
					}
				})
			}
		}
	}
}
