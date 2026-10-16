package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
)

func TestWorkflowFlowNodeCatalogueIsReadOnly(t *testing.T) {
	root := t.TempDir()
	var out, stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--nodes"}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var view struct {
		FlowNodes []struct {
			Node        int    `json:"node"`
			Description string `json:"description"`
		} `json:"flowNodes"`
		ChoiceOwner string `json:"choiceOwner"`
	}
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	nodes := definition.MainFlowNodes()
	if len(view.FlowNodes) != len(nodes) || view.ChoiceOwner != "user" {
		t.Fatal("catalogue lost the user-owned choices", out.String())
	}
	for index, node := range nodes {
		if view.FlowNodes[index].Node != index+1 || view.FlowNodes[index].Description != node.Description {
			t.Fatal("catalogue differed from definition", out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatal("catalogue created workflow state", err)
	}
}

func TestWorkflowStartSelectsFlowNodesFromPublishedPositions(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	clearHostEnv(t)
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	receipt := writerIntakeReceipt(t, root)
	intake, err := facade.SaveOriginalCandidate(workflowPayloadDirectory(root, "", "intake"), receipt)
	if err != nil {
		t.Fatal(err)
	}
	nodes := protocol.FlowNodeIDs()
	selection, err := selectFlowNodes([]int{1, len(nodes)})
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"workflow", "start", "--root", root, "--run-id", "selected-flow", "--route", "lightweight", "--intake", strconv.Itoa(intake), "--disable-node", "1", "--disable-node", strconv.Itoa(len(nodes)), "--machine"}
	if code := runWithHostConfirmation(t, args, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var run facade.Run
	if err := json.Unmarshal(out.Bytes(), &run); err != nil || !reflect.DeepEqual(run.FlowSelection, selection) {
		t.Fatal("start did not use original catalogue choices", err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(root, facade.EngineNamespace, run.RunID, "request.json"))
	var request facade.StartRequest
	if err != nil || json.Unmarshal(data, &request) != nil || !reflect.DeepEqual(request.FlowSelection, selection) || !reflect.DeepEqual(request.IntakeConfirmationReceipt, receipt) {
		t.Fatal("selection changed the original intake or lost the user choices", err)
	}
	out.Reset()
	if code := runWithHostConfirmation(t, []string{"workflow", "show", "--root", root, "--run-id", run.RunID}, IO{Stdout: &out, Stderr: &stderr}); code != 0 {
		t.Fatal(stderr.String())
	}
	var view struct {
		FlowNodes []struct {
			Enabled bool `json:"enabled"`
		} `json:"flowNodes"`
	}
	if err := json.Unmarshal(out.Bytes(), &view); err != nil || len(view.FlowNodes) != len(nodes) || view.FlowNodes[0].Enabled || view.FlowNodes[len(nodes)-1].Enabled {
		t.Fatal("read-side view lost disabled choices", err, out.String())
	}
}

func TestWorkflowStartRejectsInvalidFlowSelectionBeforePrecheck(t *testing.T) {
	for _, flags := range [][]string{
		{"--disable-node", strconv.Itoa(len(protocol.FlowNodeIDs()) + 1)},
		{"--disable-node", "1", "--disable-node", "1"},
		{"--precheck", "--disable-node", "1"},
	} {
		root := t.TempDir()
		var out, stderr bytes.Buffer
		args := append([]string{"workflow", "start", "--root", root}, flags...)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code == 0 {
			t.Fatal("invalid or premature selection was accepted", flags, out.String())
		}
		if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
			t.Fatal("invalid selection wrote workflow state", err)
		}
	}
}
