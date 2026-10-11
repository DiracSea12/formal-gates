package harness

import (
	"fmt"
	"io"
)

// ExecutionAdmission holds the engine's existing execution window until the
// native job or supervisor has claimed its original process evidence.
type ExecutionAdmission func(string, map[string]any) (func(), error)

func admitExecution(directory string, request map[string]any, admissions []ExecutionAdmission) (func(), error) {
	if len(admissions) > 0 && admissions[0] != nil {
		return admissions[0](directory, request)
	}
	if _, managed := request["engine"]; managed {
		return nil, fmt.Errorf("UNSUPPORTED_EXECUTION: engine admission is unavailable")
	}
	return nil, nil
}

// RunCommand serves the engine’s native subprocess and tool boundaries.
func RunCommand(args []string, in io.Reader, out, errout io.Writer, admissions ...ExecutionAdmission) int {
	if len(args) > 0 && args[0] == "codex-tool-permission" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness codex-tool-permission requires the original job directory")
			return 2
		}
		return CodexToolPermission(args[1], in, out, errout, admissions...)
	}
	if len(args) > 0 && args[0] == "cursor-tool-permission" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness cursor-tool-permission requires the original job directory")
			return 2
		}
		return CursorToolPermission(args[1], in, out, errout, admissions...)
	}
	if len(args) > 0 && args[0] == "native-tools" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness native-tools requires the original job directory")
			return 2
		}
		return NativeTools(args[1], in, out, errout, admissions...)
	}
	if len(args) > 0 && args[0] == "bridge" {
		return Bridge(in, out)
	}
	if len(args) > 0 && args[0] == "supervise" {
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness supervise requires a job directory")
			return 2
		}
		return Supervise(args[1], admissions...)
	}
	if len(args) > 0 && args[0] == "native" {
		// One native job, one process.
		// The entry's own argument check mirrors supervise's: exit 2 with a
		// note on stderr and no native-error.json.
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness native requires a job directory")
			return 2
		}
		return RunNative(args[1], errout, admissions...)
	}
	if len(args) > 0 && args[0] == "native-respond" {
		return NativeRespond(args[1:], errout)
	}
	if len(args) > 0 && args[0] == "native-show" {
		return NativeShow(args[1:], out, errout)
	}
	fmt.Fprintln(errout, "unknown native command")
	return 2
}
