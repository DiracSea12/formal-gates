package harness

import (
	"fmt"
	"formal-gates/internal/engine/protocol"
	"io"
)

// ExecutionServices explicitly carries the engine-owned execution boundary.
// Native adapters receive the same services during ordinary turns and recovery.
type ExecutionServices struct {
	Admit          func(string, map[string]any) (func(), error)
	ClaimMessage   func(string, string, bool) (*protocol.TaskMessage, error)
	ReceiptMessage func(string, protocol.TaskMessage) error
}

func admitExecution(directory string, request map[string]any, services []ExecutionServices) (func(), error) {
	if len(services) > 0 && services[0].Admit != nil {
		return services[0].Admit(directory, request)
	}
	if _, managed := request["engine"]; managed {
		return nil, fmt.Errorf("UNSUPPORTED_EXECUTION: engine admission is unavailable")
	}
	return nil, nil
}

// RunCommand serves the engine’s native subprocess and tool boundaries.
func RunCommand(args []string, in io.Reader, out, errout io.Writer, services ...ExecutionServices) int {
	if len(args) > 0 && (args[0] == "codex-hook-client" || args[0] == "codex-hook-target") {
		if len(args) != 2 {
			fmt.Fprintln(errout, "Codex hook maintenance requires its original evidence directory")
			return 2
		}
		if args[0] == "codex-hook-target" {
			return CodexHookTarget(args[1], in, out, errout)
		}
		if err := CodexHookClient(args[1]); err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "codex-tool-permission" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness codex-tool-permission requires the original job directory")
			return 2
		}
		return CodexToolPermission(args[1], in, out, errout, services...)
	}
	if len(args) > 0 && args[0] == "cursor-tool-permission" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness cursor-tool-permission requires the original job directory")
			return 2
		}
		return CursorToolPermission(args[1], in, out, errout, services...)
	}
	if len(args) > 0 && args[0] == "native-tools" {
		if len(args) != 2 {
			fmt.Fprintln(errout, "harness native-tools requires the original job directory")
			return 2
		}
		return NativeTools(args[1], in, out, errout, services...)
	}
	if len(args) > 0 && args[0] == "bridge" {
		return Bridge(in, out)
	}
	if len(args) > 0 && args[0] == "supervise" {
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness supervise requires a job directory")
			return 2
		}
		return Supervise(args[1], services...)
	}
	if len(args) > 0 && args[0] == "native" {
		// One native job, one process.
		// The entry's own argument check mirrors supervise's: exit 2 with a
		// note on stderr and no native-error.json.
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness native requires a job directory")
			return 2
		}
		return RunNative(args[1], errout, services...)
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
