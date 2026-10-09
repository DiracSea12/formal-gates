package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/coordination"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// strerrorText mirrors CPython's os.strerror capitalisation. Go's
// syscall.Errno.Error() lowercases the text ("no such file or directory"), so a
// naive concatenation would break the Python side's error text byte for byte.
// The exact capitalisation is observed behaviour and is preserved here.
var strerrorText = map[int]string{
	2:  "No such file or directory",
	8:  "Exec format error",
	13: "Permission denied",
	20: "Not a directory",
	21: "Is a directory",
	26: "Text file busy",
}

// exceptionName maps an errno to the Python exception class CPython raises while
// executing a subprocess. A directory is forced to PermissionError (errno 13):
// on macOS execve on a directory returns EACCES, so the reference implementation
// reports PermissionError, never IsADirectoryError.
var exceptionName = map[int]string{
	2:  "FileNotFoundError",
	8:  "OSError",
	13: "PermissionError",
	20: "NotADirectoryError",
	21: "IsADirectoryError",
	26: "OSError",
}

// defpath is os.defpath on POSIX: the PATH used when the (merged) environment
// has no PATH at all.
const defpath = "/bin:/usr/bin"

// Supervise takes execution ownership of one job directory. It claims
// the owner lock, records its
// own process identity, spawns the request's command with the merged
// environment, captures stdout/stderr to disk with an fsync, honours SIGTERM
// cancellation, and writes an exit receipt whose shape and proofs match the
// Python job runner exactly.
//
// Exit codes:
//   - 0 once exit.json has been written, regardless of the child's outcome;
//   - 2 when an owner already exists (no exit.json, no process.json, no child);
//   - any other non-zero when the supervisor itself cannot proceed
//     (missing/malformed request.json) - again with no receipt written.
func Supervise(directory string) int {
	dir := directory

	requestRaw, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervise: cannot read request.json: %v\n", err)
		return 1
	}
	var request map[string]any
	dec := json.NewDecoder(bytes.NewReader(requestRaw))
	dec.UseNumber()
	if err := dec.Decode(&request); err != nil {
		fmt.Fprintf(os.Stderr, "supervise: cannot parse request.json: %v\n", err)
		return 1
	}

	// Owner lock: O_CREAT|O_EXCL|O_WRONLY, mode 0600. A pre-existing owner means
	// another supervisor already owns this job, so back off without writing
	// anything (Store.job's poll loop will read the existing receipt).
	fd, err := os.OpenFile(filepath.Join(dir, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return 2
		}
		fmt.Fprintf(os.Stderr, "supervise: cannot claim owner: %v\n", err)
		return 1
	}
	fd.Close()

	// Process identity is written BEFORE the child starts so Store.job's liveness
	// probe (UNKNOWN_START guard, 10s window) can observe it.
	procStarted := float64(time.Now().UnixNano()) / 1e9
	process := map[string]any{"pid": os.Getpid(), "started_at": procStarted}
	if supervisedCheck(request) {
		pgid, err := nativeProcessGroupID(os.Getpid())
		if err != nil {
			return 1
		}
		digest, err := CanonicalDigest(request)
		if err != nil {
			return 1
		}
		process["request_digest"] = digest
		process["pgid"] = pgid
	}
	if err := writeAtomicJSON(filepath.Join(dir, "process.json"), process); err != nil {
		fmt.Fprintf(os.Stderr, "supervise: cannot write process.json: %v\n", err)
		return 1
	}

	started := float64(time.Now().UnixNano()) / 1e9
	code := 127
	errorMsg := ""

	argv := extractArgs(request["argv"])
	cwd := toString(request["cwd"])
	stdin := toStringDefault(request["stdin"], "")

	executionFacts := map[string]string{}
	spawnErrMsg, candidates := resolveSpawn(argv, cwd, request)
	if spawnErrMsg != "" {
		errorMsg = spawnErrMsg
		ensureEmptyStream(filepath.Join(dir, "stdout.log"))
		ensureEmptyStream(filepath.Join(dir, "stderr.log"))
	} else {
		code, errorMsg = runChild(dir, candidates, argv, cwd, stdin, request, executionFacts)
	}

	digest, err := CanonicalDigest(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervise: cannot digest request: %v\n", err)
		return 1
	}
	stdoutProof, _ := FileProof(filepath.Join(dir, "stdout.log"))
	stderrProof, _ := FileProof(filepath.Join(dir, "stderr.log"))
	receipt := map[string]any{
		"exit_code":      code,
		"error":          errorMsg,
		"started_at":     started,
		"finished_at":    float64(time.Now().UnixNano()) / 1e9,
		"request_digest": digest,
		"streams": map[string]any{
			"stdout.log": proofToMap(stdoutProof),
			"stderr.log": proofToMap(stderrProof),
		},
	}
	if supervisedCheck(request) {
		receipt["tool_identity"], receipt["environment_identity"] = executionFacts["tool"], executionFacts["environment"]
	}
	if err := writeAtomicJSON(filepath.Join(dir, "exit.json"), receipt); err != nil {
		fmt.Fprintf(os.Stderr, "supervise: cannot write exit.json: %v\n", err)
		return 1
	}
	return 0
}

// resolveSpawn performs every validation CPython's subprocess.Popen does before
// exec and returns the ordered list of PATH candidates to try, or a Python-shaped
// error string (and a nil candidate list) when the spawn must fail with code 127.
//
// The unreachable branches below (empty argv, embedded NUL, environment value
// type, '=' in an environment key) are reproduced exactly because they are the
// kind of "type-boundary silent narrowing" the migration is required to preserve;
// ValidateSpec guarantees they cannot occur, so they are never exercised by real
// requests, but the text is kept byte-identical to the measured reference.
func resolveSpawn(argv []string, cwd string, request map[string]any) (string, []string) {
	if len(argv) == 0 {
		return "IndexError: list index out of range", nil
	}
	for _, a := range argv {
		if strings.ContainsRune(a, '\x00') {
			return "ValueError: embedded null byte", nil
		}
	}
	if cwd != "" {
		info, err := os.Stat(cwd)
		if err != nil {
			// os.chdir on a missing path raises FileNotFoundError; on a
			// permission failure it would raise PermissionError, but the only
			// reachable shape here (a missing clone dir) is ENOENT.
			return spawnErr("FileNotFoundError", 2, cwd), nil
		}
		if !info.IsDir() {
			return spawnErr("NotADirectoryError", 20, cwd), nil
		}
	}
	if envRaw, ok := request["environment"].(map[string]any); ok {
		for k := range envRaw {
			if strings.ContainsRune(k, '=') {
				return "ValueError: illegal environment variable name", nil
			}
		}
		for _, v := range envRaw {
			if _, ok := v.(string); !ok {
				return "TypeError: expected str, bytes or os.PathLike object, not int", nil
			}
		}
	}

	// A bare argv[0] is resolved against the merged environment's PATH; an
	// argv[0] containing a separator is an explicit path and is tried as-is
	// (single candidate, no PATH walk). The error text (recorded at exec time,
	// in runChild) always names argv[0] exactly, never the resolved path.
	name := argv[0]
	if strings.ContainsRune(name, os.PathSeparator) {
		return "", []string{name}
	}
	candidates := searchCandidates(name, mergedEnvMap(request))
	if len(candidates) == 0 {
		return spawnErr("FileNotFoundError", 2, name), nil
	}
	return "", candidates
}

// childSignalGrace is the bounded window runChild grants the SIGTERM channel
// after the child has been reaped by a signal, to confirm whether the supervisor
// itself also received the (process-group) SIGTERM. If so the receipt is a
// cancellation (130); otherwise the child's own -N is reported unchanged. The
// common non-cancelled path (a normal exit, or a child death by signal the
// supervisor did not share) is unaffected except for this short wait in the
// latter sub-case.
const childSignalGrace = 250 * time.Millisecond

// runChild walks the candidate list (in PATH order), relaying stdin and capturing
// stdout/stderr to the job's stream files, and returns the receipt's exit code and
// error text. It reproduces CPython's execvp semantics: each candidate is tried in
// turn; an ENOENT candidate is skipped (nothing recorded); an EACCES or ENOEXEC
// candidate records the first such error and is skipped; the first candidate that
// actually executes wins. The child's merged environment is the parent environment
// updated with the request's environment.
func runChild(dir string, candidates []string, argv []string, cwd, stdin string, request map[string]any, facts map[string]string) (int, string) {
	outF, err := os.OpenFile(filepath.Join(dir, "stdout.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 127, spawnErr("OSError", 0, argv[0])
	}
	errF, err := os.OpenFile(filepath.Join(dir, "stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		outF.Close()
		return 127, spawnErr("OSError", 0, argv[0])
	}
	defer func() {
		outF.Sync()
		errF.Sync()
		outF.Close()
		errF.Close()
	}()

	// Cancellation: Store.cancel_jobs sends SIGTERM to the supervisor's process
	// group (the supervisor is the session leader via start_new_session). The
	// signal handler is installed here, BEFORE the first child is started, so no
	// new race window is introduced. We only forward the signal to the direct
	// child, never broadcast to our own group.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var firstErr string
	for _, candidate := range candidates {
		toolPath := candidate
		if !filepath.IsAbs(toolPath) {
			toolPath = filepath.Join(cwd, toolPath)
		}
		toolProof, _ := FileProof(toolPath)
		code, errorMsg, ran := tryExec(candidate, argv, cwd, stdin, request, outF, errF, sigCh)
		if ran {
			if facts != nil {
				facts["tool"] = toolPath + " sha256:" + toolProof.SHA256
				environment := map[string]any{}
				for key, value := range mergedEnvMap(request) {
					environment[key] = value
				}
				var err error
				facts["environment"], err = CanonicalDigest(environment)
				if err != nil {
					return 127, err.Error()
				}
			}
			return code, errorMsg
		}
		// Not ran: either ENOENT (continue, not recorded) or a recorded error.
		if errorMsg != "" && firstErr == "" {
			firstErr = errorMsg
		}
	}
	if firstErr != "" {
		return 127, firstErr
	}
	return 127, spawnErr("FileNotFoundError", 2, argv[0])
}

// tryExec attempts to start one PATH candidate. It returns ran=false when the
// exec did not happen (ENOENT, or a recorded EACCES/ENOEXEC error that should let
// the caller continue to the next candidate) and ran=true once a child has been
// started and reaped, in which case code/errorMsg are the receipt values.
func tryExec(candidate string, argv []string, cwd, stdin string, request map[string]any, outF, errF *os.File, sigCh chan os.Signal) (int, string, bool) {
	cmd := &exec.Cmd{
		Path:   candidate,
		Args:   argv,
		Dir:    cwd,
		Env:    mergedEnvList(request),
		Stdin:  bytes.NewReader([]byte(stdin)),
		Stdout: outF,
		Stderr: errF,
	}
	if supervisedCheck(request) {
		cmd.SysProcAttr = supervisorSysProcAttr()
	}
	if startErr := cmd.Start(); startErr != nil {
		var errno syscall.Errno
		if errors.As(startErr, &errno) && errno == syscall.ENOENT {
			// Nothing recorded for ENOENT; the caller continues the PATH walk.
			return 0, "", false
		}
		// EACCES, ENOEXEC, and any other errno: record the first and continue.
		return 0, mapExecError(startErr, argv[0]), false
	}
	if supervisedCheck(request) {
		defer signalSupervisedChild(cmd, true)
	}
	code, errorMsg, cancelled := waitChild(cmd, sigCh, supervisedCheck(request))
	if cancelled {
		return 130, "owned job was cancelled", true
	}
	return code, errorMsg, true
}

// waitChild reaps the started child on a goroutine and settles the receipt. It
// prefers the supervisor's SIGTERM: if SIGTERM arrives while waiting, the receipt
// is a deterministic cancellation (130, "owned job was cancelled"), even when the
// same process-group signal also killed the child. If the child dies by a signal
// and no SIGTERM reached the supervisor within childSignalGrace, the child's own
// -N is reported unchanged (including -15 with an empty error and -9 for SIGKILL).
func waitChild(cmd *exec.Cmd, sigCh chan os.Signal, grouped bool) (int, string, bool) {
	type status struct {
		signaled bool
		sig      int
		exitCode int
	}
	resCh := make(chan status, 1)
	go func() {
		cmd.Wait()
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		var s status
		if ws.Signaled() {
			s.signaled = true
			s.sig = int(ws.Signal())
		} else {
			s.exitCode = ws.ExitStatus()
		}
		resCh <- s
	}()

	terminate := func() {
		if grouped {
			signalSupervisedChild(cmd, false)
		} else {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-resCh:
		case <-time.After(5 * time.Second):
			if grouped {
				signalSupervisedChild(cmd, true)
			} else {
				_ = cmd.Process.Kill()
			}
			<-resCh
		}
	}
	select {
	case <-sigCh:
		// Supervisor cancelled while waiting. Forward SIGTERM to the direct
		// child (it may already be dead from the group signal) and settle with a
		// 5s kill timeout, exactly as the reference's terminate handler does.
		terminate()
		return 130, "owned job was cancelled", true
	case s := <-resCh:
		if s.signaled {
			// The child died by a signal. Give the supervisor's signal channel a
			// bounded moment to confirm whether the same group SIGTERM also
			// reached the supervisor; if so this is a cancellation.
			select {
			case <-sigCh:
				return 130, "owned job was cancelled", true
			case <-time.After(childSignalGrace):
				return -s.sig, "", false
			}
		}
		return s.exitCode, "", false
	}
}

// mergedEnvMap returns the parent environment updated with the request's
// environment. The PATH used for bare-name argv[0] resolution is read from this
// merged map, never from the parent process's PATH.
func mergedEnvMap(request map[string]any) map[string]string {
	merged := map[string]string{}
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i >= 0 {
			merged[e[:i]] = e[i+1:]
		}
	}
	if envRaw, ok := request["environment"].(map[string]any); ok {
		for k, v := range envRaw {
			if vs, ok := v.(string); ok {
				merged[k] = vs
			}
		}
	}
	return merged
}

// mergedEnvList is mergedEnvMap rendered as the "KEY=VALUE" slice exec expects.
func mergedEnvList(request map[string]any) []string {
	merged := mergedEnvMap(request)
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

// searchCandidates enumerates the PATH candidates for a bare argv[0], in order,
// against the merged environment's PATH, falling back to /bin:/usr/bin when PATH
// is absent. It deliberately does NOT pre-filter by executability: CPython's
// execvp (and therefore the reference) walks the list and tries to execve each
// candidate, recording the first EACCES/ENOEXEC and continuing, so the kernel's
// own errno — not a pre-flight Stat — decides which candidate runs. runChild
// performs that walk.
func searchCandidates(name string, env map[string]string) []string {
	pathEnv, ok := env["PATH"]
	if !ok {
		pathEnv = defpath
	}
	out := make([]string, 0, strings.Count(pathEnv, ":")+1)
	for _, dir := range strings.Split(pathEnv, ":") {
		var cand string
		if dir == "" {
			cand = "./" + name
		} else {
			cand = filepath.Join(dir, name)
		}
		out = append(out, cand)
	}
	return out
}

func spawnErr(name string, errno int, filename string) string {
	msg := strerrorText[errno]
	if msg == "" {
		msg = "Unknown error"
	}
	return fmt.Sprintf("%s: [Errno %d] %s: '%s'", name, errno, msg, filename)
}

// mapExecError renders Go's exec start failure using CPython's exception text.
// The reachable errnos are ENOENT (2), EACCES (13) and ENOEXEC (8); any other
// errno falls back to a generic OSError so the shape stays {Name}: [{Errno}] ...
func mapExecError(err error, argv0 string) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if name, ok := exceptionName[int(errno)]; ok {
			return fmt.Sprintf("%s: [Errno %d] %s: '%s'", name, int(errno), strerrorText[int(errno)], argv0)
		}
		return fmt.Sprintf("OSError: [Errno %d] %s: '%s'", int(errno), errno.Error(), argv0)
	}
	return "OSError: " + err.Error()
}

func extractArgs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toStringDefault(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func proofToMap(p ContentProof) map[string]any {
	return map[string]any{"sha256": p.SHA256, "size": p.Size}
}

// writeAtomicJSON serialises value with the single canonical encoder and writes
// it atomically (temp file, fsync, rename). The value may be a typed record;
// canonicalBody routes it through its JSON tags into the canonical encoder, so
// the receipt is byte-identical to the Python reference's canonical() output
// and no second JSON encoding is introduced.
func writeAtomicJSON(path string, value any) error {
	encoded, err := canonicalBody(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ensureEmptyStream(path string) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	f.Sync()
	f.Close()
}

func supervisedCheck(request map[string]any) bool {
	kind, _ := request["kind"].(string)
	return kind == "approved-development-check" || kind == "approved-qa-check"
}

// supervisorAlive matches the recorded PID and creation time to a live process.
func supervisorAlive(turnDir string) bool {
	value, err := readJSONFile(filepath.Join(turnDir, "process.json"))
	if err != nil {
		return false
	}
	pid := intOf(getOf(value, "pid"))
	if pid <= 0 {
		return false
	}
	if !coordination.ProcessAlive(pid) {
		return false
	}
	started := floatOf(getOf(value, "started_at"))
	if started == 0 {
		return false
	}
	created, ok := processCreationTime(pid)
	if !ok {
		return false
	}
	delta := created.Unix() - int64(started)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2
}
