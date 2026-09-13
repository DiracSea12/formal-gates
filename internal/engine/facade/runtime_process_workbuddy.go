package facade

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

// runWorkBuddyProcess consumes an adapter-built command only after its caller
// has claimed the action. The job directory is the engine-assigned invocation
// directory. Logs and process completion are kept even when result parsing fails.
func runWorkBuddyProcess(ctx context.Context, command *exec.Cmd, job, actionID, expectedIdentity, prompt string, attached func(string) error) (workBuddyResult, error) {
	result, err := runNativeStreamProcess(ctx, command, job, actionID, func(reader io.Reader, stdin io.WriteCloser) (nativeProcessResult, error) {
		_, writeErr := io.WriteString(stdin, prompt)
		closeErr := stdin.Close()
		result, err := observeWorkBuddyStream(reader, expectedIdentity, attached)
		return nativeProcessResult{Identity: result.Identity, Answer: result.Answer}, errors.Join(writeErr, closeErr, err)
	})
	return workBuddyResult{Identity: result.Identity, Answer: result.Answer}, err
}
