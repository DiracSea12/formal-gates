//go:build !windows

package harness

import (
	"errors"
	"fmt"
	"syscall"
)

// nativeProcessGroupID reports the child's process group the way the reference
// implementation does (os.getpgid(pid)): the spawn never creates a new group,
// so the value equals the caller's own pgid. A child that already exited and
// was reaped raises the observable ProcessLookupError text; any other errno
// keeps the generic OSError shape.
func nativeProcessGroupID(pid int) (int, error) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.ESRCH {
			return 0, &pyError{name: "ProcessLookupError", message: "[Errno 3] No such process"}
		}
		return 0, &pyError{name: "OSError", message: fmt.Sprintf("[Errno %d] %s", int(errno), errno.Error())}
	}
	return pgid, nil
}
