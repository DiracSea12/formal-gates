//go:build !windows

package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

func TestDshPluginProviderReachesOriginalStartOwner(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	root := t.TempDir()
	launcher := filepath.Join(root, ".local", "bin", "formal-gates")
	if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\ncat > \"$CAPTURE_PAYLOAD\"\nprintf '{}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	plugin, err := filepath.Abs("dsh_plugin.mjs")
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(root, "owner.jsonl")
	driver := filepath.Join(root, "driver.mjs")
	if err := os.WriteFile(driver, []byte(`import { pathToFileURL } from 'node:url'
const [plugin, root, transcript, provider] = process.argv.slice(2)
const { apply } = await import(pathToFileURL(plugin))
let listener
const ctx = {
  logger: {warn(message) {throw new Error(message)}},
  on(event, callback) {if (event !== 'tools/pre-execute') throw new Error(event); listener = callback},
  get(name) {if (name !== 'sessionPersistence') throw new Error(name); return {locate() {return {path: transcript}}}}
}
apply(ctx, {provider})
let continued = false
await listener({name: 'Bash', arguments: {command: 'formal-gates workflow start --root ' + root + ' --run-id regular'}, agent: {session: {header: {id: 'dsh-owner', cwd: root}}}}, () => {continued = true})
if (!continued) throw new Error('normal start was not continued')
`), 0600); err != nil {
		t.Fatal(err)
	}
	argsPath, payloadPath := filepath.Join(root, "args.txt"), filepath.Join(root, "payload.json")
	cmd := exec.Command(node, driver, plugin, root, transcript, host.DeepSeek)
	cmd.Env = append(os.Environ(), "HOME="+root, "CAPTURE_ARGS="+argsPath, "CAPTURE_PAYLOAD="+payloadPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("original plugin failed: %v: %s", err, output)
	}
	argsBody, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(argsBody)), "\n")
	if !reflect.DeepEqual(argv, []string{"hook", "decide", "--provider", host.DeepSeek}) {
		t.Fatal("original plugin omitted its configured provider", argv)
	}
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(payload, &original); err != nil || original["session_id"] != "dsh-owner" || original["transcript_path"] != transcript || original["cwd"] != root {
		t.Fatal("original plugin changed owner evidence", err, original)
	}
	if _, err := Hook(payload, argv[3]); err != nil {
		t.Fatal(err)
	}
	gotTranscript, gotSession, gotProvider := ConsumeStartOwner(root, "regular")
	if gotTranscript != transcript || gotSession != "dsh-owner" || gotProvider != host.DeepSeek {
		t.Fatalf("original start owner lost: %q %q %q", gotTranscript, gotSession, gotProvider)
	}
	t.Logf("actual plugin argv=%q; original Hook/ConsumeStartOwner provider=%q session=%q transcript=%q", argv, gotProvider, gotSession, gotTranscript)
}
