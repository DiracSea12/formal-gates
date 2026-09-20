package harness

// The G2d interaction CLI: `formal-gates harness native-respond` writes one
// user decision for a held native interaction, and `formal-gates harness
// native-show` lists a job's held interactions read-only. Both work entirely
// on the job's evidence directory: the running native job process polls the
// answer file and delivers the reply in the host's own frame shape under the
// original request id. Answering is never a process approval of any kind.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// nativePendingPath is the held-interaction evidence file both commands read.
func nativePendingPath(directory string) string {
	return filepath.Join(directory, "interaction-pending.json")
}

// nativeAnswerPath is the decision file native-respond writes and the job
// process consumes by rename.
func nativeAnswerPath(directory string) string {
	return filepath.Join(directory, "interaction-answer.json")
}

// readNativePendings loads the pending records; a missing file is an empty
// list (nothing was ever held), anything unreadable is the caller's error.
func readNativePendings(directory string) ([]any, error) {
	value, err := readJSONFile(nativePendingPath(directory))
	if err != nil {
		if os.IsNotExist(err) {
			return []any{}, nil
		}
		return nil, err
	}
	records, isList := value.([]any)
	if !isList {
		return nil, fmt.Errorf("%s must contain a JSON array", nativePendingPath(directory))
	}
	return records, nil
}

// nativePendingOffers reports whether the host's original options contain the
// id the user chose. Host option objects identify themselves by "optionId"
// (ACP permissions, ZCode permissions) or by "value" (ZCode question options)
// - both spellings are the hosts' own, reverse-read from their protocol
// schemas; nothing is invented here.
func nativePendingOffers(options any, want string) bool {
	for _, option := range listOf(options) {
		if valuesEqual(getOf(option, "optionId"), want) || valuesEqual(getOf(option, "value"), want) {
			return true
		}
	}
	return false
}

// offeredIDs renders the host-given ids for an error message.
func offeredIDs(options any) string {
	ids := ""
	for _, option := range listOf(options) {
		id := getOf(option, "optionId")
		if id == nil {
			id = getOf(option, "value")
		}
		if ids != "" {
			ids += ", "
		}
		ids += textOf(id)
	}
	if ids == "" {
		return "(the host offered no options)"
	}
	return ids
}

// NativeRespond is `formal-gates harness native-respond --job <dir>
// --request <id> (--option <optionId> | --cancel)`: validate that the request
// is still pending and that the chosen option is one the host offered, then
// atomically write interaction-answer.json. The job process consumes the file
// exactly once (rename) and replies to the original connection.
func NativeRespond(args []string, errout io.Writer) int {
	job := ""
	request := ""
	option := ""
	cancel := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--job":
			if i+1 >= len(args) {
				fmt.Fprintln(errout, "--job requires a directory")
				return 2
			}
			i++
			job = args[i]
		case "--request":
			if i+1 >= len(args) {
				fmt.Fprintln(errout, "--request requires a request id")
				return 2
			}
			i++
			request = args[i]
		case "--option":
			if i+1 >= len(args) {
				fmt.Fprintln(errout, "--option requires an option id")
				return 2
			}
			i++
			option = args[i]
		case "--cancel":
			cancel = true
		default:
			fmt.Fprintf(errout, "unknown argument %q\n", args[i])
			return 2
		}
	}
	if job == "" || request == "" {
		fmt.Fprintln(errout, "usage: formal-gates harness native-respond --job <dir> --request <id> (--option <optionId> | --cancel)")
		return 2
	}
	if cancel == (option != "") {
		fmt.Fprintln(errout, "answer with exactly one of --option <optionId> or --cancel")
		return 2
	}
	directory := resolveNativePath(job)
	info, statErr := os.Stat(directory)
	if statErr != nil || !info.IsDir() {
		fmt.Fprintf(errout, "job directory not found: %s\n", job)
		return 1
	}
	pendings, err := readNativePendings(directory)
	if err != nil {
		fmt.Fprintf(errout, "%v\n", err)
		return 1
	}
	var held map[string]any
	for _, record := range pendings {
		entry, isObject := record.(map[string]any)
		if isObject && valuesEqual(getOf(entry, "request_id"), request) {
			held = entry
		}
	}
	if held == nil {
		fmt.Fprintf(errout, "no pending interaction with request id %s in %s\n", request, job)
		return 1
	}
	if textOf(getOf(held, "status")) != "pending" {
		fmt.Fprintf(errout, "request %s is %s, not pending\n", request, textOf(getOf(held, "status")))
		return 1
	}
	if option != "" && !nativePendingOffers(getOf(held, "options"), option) {
		fmt.Fprintf(errout, "option %s is not one the host offered; offered: %s\n", option, offeredIDs(getOf(held, "options")))
		return 1
	}
	if _, statErr := os.Stat(nativeAnswerPath(directory)); statErr == nil {
		fmt.Fprintln(errout, "interaction-answer.json already exists; the job has not consumed the previous answer yet")
		return 1
	}
	answer := map[string]any{
		"request_id": request,
		"cancel":     cancel,
		"written_at": time.Now().UTC().Format(time.RFC3339),
	}
	if option != "" {
		answer["option"] = option
	}
	if err := writeAtomicJSON(nativeAnswerPath(directory), answer); err != nil {
		fmt.Fprintf(errout, "cannot write the answer: %v\n", err)
		return 1
	}
	return 0
}

// NativeShow is `formal-gates harness native-show --job <dir>`: read-only
// listing of the job's held native interactions, one canonical JSON line.
func NativeShow(args []string, out, errout io.Writer) int {
	job := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--job":
			if i+1 >= len(args) {
				fmt.Fprintln(errout, "--job requires a directory")
				return 2
			}
			i++
			job = args[i]
		default:
			fmt.Fprintf(errout, "unknown argument %q\n", args[i])
			return 2
		}
	}
	if job == "" {
		fmt.Fprintln(errout, "usage: formal-gates harness native-show --job <dir>")
		return 2
	}
	directory := resolveNativePath(job)
	info, statErr := os.Stat(directory)
	if statErr != nil || !info.IsDir() {
		fmt.Fprintf(errout, "job directory not found: %s\n", job)
		return 1
	}
	pendings, err := readNativePendings(directory)
	if err != nil {
		fmt.Fprintf(errout, "%v\n", err)
		return 1
	}
	encoded, err := CanonicalJSON(pendings)
	if err != nil {
		fmt.Fprintf(errout, "%v\n", err)
		return 1
	}
	fmt.Fprintln(out, string(encoded))
	return 0
}
