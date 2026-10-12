package harness

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceActionDeclarationsUseOriginalRPC(t *testing.T) {
	for _, action := range []string{"list", "read", "search", "requirement", "replace", "copy-range", "create", "mkdir", "remove", "copy", "move", "executable"} {
		t.Run(action, func(t *testing.T) {
			directory, workspace := nativeToolFixture(t)
			for name, body := range map[string]string{"source.txt": "alpha\nbeta\n", "target.txt": "zero\n"} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
			if err != nil {
				t.Fatal(err)
			}
			materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
			material := worker.Material{Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "issued-action"}, Materials: []protocol.IntakeMaterial{{Path: "requirements.md", Content: "# Frozen\nalpha\n", Revision: "req-source"}, {Path: "design.md", Content: "# Frozen design\nbeta\n", Revision: "design-source"}}}}
			material.Input.RequirementDocument.Path = "requirements.md"
			material.Input.RequirementDocument.Content = "# Frozen\nalpha\n"
			material.Input.RequirementDocument.ContentDigest = "req-source"
			if err := writeAtomicJSON(materialPath, material); err != nil {
				t.Fatal(err)
			}
			rpc := func(method string, args map[string]any) map[string]any {
				t.Helper()
				frame := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
				if method == "tools/call" {
					frame["params"] = map[string]any{"name": "workspace", "arguments": args}
				}
				body, _ := json.Marshal(frame)
				var out, errout bytes.Buffer
				if code := NativeTools(directory, bytes.NewReader(append(body, '\n')), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
					t.Fatal(errout.String())
				}
				reply, err := decodeNativeJSON(out.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				return objectOrEmpty(getOf(reply, "result"))
			}
			listing := rpc("tools/call", map[string]any{"action": "list"})
			var roots map[string]any
			if err = json.Unmarshal([]byte(textOf(getOf(listOf(listing["content"])[0], "text"))), &roots); err != nil {
				t.Fatal(err)
			}
			entries := listOf(roots["entries"])
			selectEntry := func(name string) any {
				for _, entry := range entries {
					if getOf(entry, "name") == name {
						return getOf(entry, "entry")
					}
				}
				t.Fatal("original entry missing", name)
				return nil
			}
			read := func(name string) any {
				value := rpc("tools/call", map[string]any{"action": "read", "listing": roots["listing"], "entry": selectEntry(name)})
				var answer map[string]any
				if err = json.Unmarshal([]byte(textOf(getOf(listOf(value["content"])[0], "text"))), &answer); err != nil {
					t.Fatal(err)
				}
				return answer["read"]
			}
			args := map[string]any{"action": action}
			invalid := map[string]any{"action": action}
			switch action {
			case "list":
				invalid["listing"] = roots["listing"]
				invalid["entry"] = 999
			case "read":
				args["listing"] = roots["listing"]
				args["entry"] = selectEntry("source.txt")
			case "search":
				args["query"] = "alpha"
			case "requirement":
				args["material"] = 2
				invalid["material"] = 999
			case "replace":
				args["read"] = read("target.txt")
				args["content"] = "changed\n"
				args["from"], args["to"] = 1, 1
			case "copy-range":
				args["read"] = read("target.txt")
				args["sourceRead"] = read("source.txt")
				args["sourceFrom"] = 1
				args["sourceTo"] = 1
				args["from"] = 1
				args["to"] = 1
			case "create":
				args["name"] = "created.txt"
				args["content"] = "created bytes\n"
			case "mkdir":
				args["name"] = "created-directory"
			case "remove", "copy", "move":
				args["listing"] = roots["listing"]
				args["entry"] = selectEntry("source.txt")
				if action != "remove" {
					args["name"] = action + ".txt"
				}
			case "executable":
				args["read"] = read("source.txt")
				args["executable"] = true
			}
			declared := rpc("tools/list", nil)
			tools := listOf(declared["tools"])
			schema := objectOrEmpty(getOf(tools[0], "inputSchema"))
			if schema["properties"] != nil {
				t.Fatal("wide parallel action properties remained")
			}
			found := false
			for _, raw := range listOf(schema["oneOf"]) {
				branch := objectOrEmpty(raw)
				props := objectOrEmpty(branch["properties"])
				if getOf(props["action"], "const") != action {
					continue
				}
				if branch["additionalProperties"] != false {
					t.Fatal("action variant is not closed")
				}
				valid := true
				for key := range args {
					if props[key] == nil {
						valid = false
					}
				}
				for _, key := range listOf(branch["required"]) {
					if _, exists := args[textOf(key)]; !exists {
						valid = false
					}
				}
				if valid {
					found = true
				}
			}
			if !found {
				t.Fatal("original valid invocation has no complete declared action branch", args)
			}
			if value := rpc("tools/call", invalid); value["isError"] != true {
				t.Fatal("invalid original action arguments accepted", invalid, value)
			}
			if value := rpc("tools/call", args); value["isError"] != false {
				t.Fatal("declared original invocation rejected", args, value)
			}
			switch action {
			case "replace", "copy-range":
				expected := "changed\n"
				if action == "copy-range" {
					expected = "alpha\n"
				}
				body, err := os.ReadFile(filepath.Join(workspace, "target.txt"))
				if err != nil || string(body) != expected {
					t.Fatal("original edited bytes", string(body), err)
				}
			case "create", "copy", "move":
				name := textOf(args["name"])
				body, err := os.ReadFile(filepath.Join(workspace, name))
				expected := "alpha\nbeta\n"
				if action == "create" {
					expected = "created bytes\n"
				}
				if err != nil || string(body) != expected {
					t.Fatal("original created/transferred bytes", string(body), err)
				}
			case "remove":
				if _, err := os.Stat(filepath.Join(workspace, "source.txt")); !os.IsNotExist(err) {
					t.Fatal("original selection was not removed")
				}
			case "executable":
				info, err := os.Stat(filepath.Join(workspace, "source.txt"))
				if err != nil || info.Mode()&0111 != 0111 {
					t.Fatal("original executable bits", err)
				}
			}
			projected := zcodeSemanticToolSchema(schema)
			if !valuesEqual(projected["oneOf"], schema["oneOf"]) || getOf(projected, "properties") == nil {
				t.Fatal("ZCode projection changed original closed action alternatives")
			}
		})
	}
}
