package facade

import (
	"context"
	"io"
	"os/exec"
)

// runZCodeProcess combines the native protocol with the same engine-owned
// process journal used by WorkBuddy. It does not claim, schedule or repeat work.
func runZCodeProcess(ctx context.Context, command *exec.Cmd, job, actionID string, session zcodeRequest, inputID, prompt string, attached func(string) error, options ...zcodeSessionOptions) (nativeProcessResult, error) {
	return runNativeStreamProcess(ctx, command, job, actionID, func(reader io.Reader, stdin io.WriteCloser) (nativeProcessResult, error) {
		identity, answer, err := runZCodeSession(reader, stdin, session, inputID, prompt, attached, options...)
		return nativeProcessResult{Identity: identity, Answer: answer}, err
	})
}
