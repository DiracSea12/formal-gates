package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"formal-gates/internal/engine/facade"
)

// Payload files carry original input and confirmation evidence, never workflow state.
func workflowPayloadDirectory(root, run, kind string) string {
	if run == "" {
		return filepath.Join(root, ".gates", "tmp", "intake")
	}
	return filepath.Join(root, facade.EngineNamespace, run, "payloads", kind)
}

func selectOriginalPayload(directory string, position int, out io.Writer) (string, bool, error) {
	paths, err := facade.OriginalPayloadPaths(directory)
	if err != nil {
		return "", true, err
	}
	if position == 0 && len(paths) == 1 {
		position = 1
	}
	if position == 0 {
		_, err := listOriginalPayloads(directory, out)
		return "", false, err
	}
	if position < 1 || position > len(paths) {
		return "", true, fmt.Errorf("select an original displayed payload position")
	}
	return paths[position-1], true, nil
}

func printPayloadValue(out io.Writer, value any, name string, position int) (int, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return 1, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return 1, err
	}
	if position != 0 {
		fields[name] = json.RawMessage(strconv.Itoa(position))
	}
	return printJSON(out, fields)
}

func workerTextDirectory(f *facade.Facade, root, runID string, worker int) (string, error) {
	run, err := f.Show()
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(run.WorkerMaterials))
	for id := range run.WorkerMaterials {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if worker < 1 || worker > len(ids) {
		return "", fmt.Errorf("select a current displayed worker")
	}
	material := run.WorkerMaterials[ids[worker-1]]
	return filepath.Join(workflowPayloadDirectory(root, runID, "messages"), string(material.ActionID), string(material.AttemptID)), nil
}

type positionListFlag []int

func (f *positionListFlag) String() string { return fmt.Sprint([]int(*f)) }
func (f *positionListFlag) Set(value string) error {
	position, err := strconv.Atoi(value)
	if err != nil || position < 1 {
		return fmt.Errorf("select a positive displayed position")
	}
	*f = append(*f, position)
	return nil
}

func listOriginalPayloads(directory string, out io.Writer) (int, error) {
	paths, err := facade.OriginalPayloadPaths(directory)
	if err != nil {
		return 1, err
	}
	choices := []map[string]any{}
	for i, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return 1, err
		}
		var content any = string(body)
		if json.Valid(body) {
			content = json.RawMessage(body)
		}
		choices = append(choices, map[string]any{"position": i + 1, "content": content})
	}
	return printJSON(out, choices)
}
