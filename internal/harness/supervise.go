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
	"runtime"
	"strings"
	"syscall"
	"time"
)

// strerrorText holds the error receipt’s exact OS message capitalisation.
var strerrorText = map[int]string{
	2:  "No such file or directory",
	8:  "Exec format error",
	13: "Permission denied",
	20: "Not a directory",
	21: "Is a directory",
	26: "Text file busy",
}

// exceptionName maps errno to the error receipt’s exception family.
// Executing a directory reports PermissionError, matching its EACCES result.
var exceptionName = map[int]string{
	2:  "FileNotFoundError",
	8:  "OSError",
	13: "PermissionError",
	20: "NotADirectoryError",
	21: "IsADirectoryError",
	26: "OSError",
}

// defpath is the PATH used when the merged environment has no PATH.
const defpath = "/bin:/usr/bin"

// Supervise takes execution ownership of one job directory. It claims
// the owner lock, records its
// own process identity, spawns the request's command with the merged
// environment, captures stdout/stderr to disk with an fsync, honours native
// cancellation, and writes the original request-bound exit receipt and proofs.
//
// Exit codes:
//   - 0 once exit.json has been written, regardless of the child's outcome;
//   - 2 when an owner already exists (no exit.json, no process.json, no child);
//   - any other non-zero when the supervisor itself cannot proceed
//     (missing/malformed request.json) - again with no receipt written.
func Supervise(directory string, services ...ExecutionServices) int {
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
	if _, err := os.Stat(filepath.Join(dir, "owner")); err == nil {
		return 2
	} else if !os.IsNotExist(err) {
		return 1
	}
	release, err := admitExecution(dir, request, services)
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervise: %v\n", err)
		return 1
	}
	if release != nil {
		defer func() {
			if release != nil {
				release()
			}
		}()
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sigCh)

	// Owner lock: O_CREAT|O_EXCL|O_WRONLY, mode 0600. A pre-existing owner means
	// another supervisor already owns this job, so back off without writing
	// anything; the native execution consumer observes the original receipt.
	if err := claimJob(dir, request); err != nil {
		if os.IsExist(err) {
			return 2
		}
		fmt.Fprintf(os.Stderr, "supervise: cannot claim owner: %v\n", err)
		return 1
	}
	if release != nil {
		release()
		release = nil
	}

	if _, err := executeRequest(dir, request, sigCh); err != nil {
		fmt.Fprintf(os.Stderr, "supervise: %v\n", err)
		return 1
	}
	return 0
}

// executeRequest uses the native runner and writes its request-bound exit facts.
func executeRequest(dir string, request map[string]any, sigCh <-chan os.Signal) (map[string]any, error) {
	started := float64(time.Now().UnixNano()) / 1e9
	code := 127
	errorMsg := ""

	argv := extractArgs(request["argv"])
	cwd := toString(request["cwd"])
	stdin := toString(request["stdin"])

	executionFacts := map[string]string{}
	spawnErrMsg, candidates := resolveSpawn(argv, cwd, request)
	if spawnErrMsg != "" {
		errorMsg = spawnErrMsg
		ensureEmptyStream(filepath.Join(dir, "stdout.log"))
		ensureEmptyStream(filepath.Join(dir, "stderr.log"))
	} else {
		code, errorMsg = runChild(dir, candidates, argv, cwd, stdin, request, executionFacts, sigCh)
	}

	digest, err := CanonicalDigest(request)
	if err != nil {
		return nil, err
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
	if supervisedOwnedJob(request) {
		receipt["tool_identity"], receipt["environment_identity"] = executionFacts["tool"], executionFacts["environment"]
	}
	if err := writeAtomicJSON(filepath.Join(dir, "exit.json"), receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

// claimJob is shared by supervisors and direct native jobs. The original owner
// is exclusive; missing completion evidence never grants another execution.
func claimJob(directory string, request map[string]any) error {
	fd, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := fd.Close(); err != nil {
		return err
	}
	created := time.Now()
	if actual, ok := processCreationTime(os.Getpid()); ok {
		created = actual
	}
	process := map[string]any{"pid": os.Getpid(), "started_at": float64(created.UnixNano()) / 1e9}
	if supervisedOwnedJob(request) {
		pgid, err := nativeProcessGroupID(os.Getpid())
		if err != nil {
			return err
		}
		digest, err := CanonicalDigest(request)
		if err != nil {
			return err
		}
		process["request_digest"], process["pgid"] = digest, pgid
	}
	return writeAtomicJSON(filepath.Join(directory, "process.json"), process)
}

// resolveSpawn validates argv, cwd and environment before exec. It returns the
// ordered PATH candidates or the error receipt text for an exit-127 spawn failure.
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
	// argv[0] containing a separator is an explicit path with no PATH walk.
	// The error text (recorded at exec time,
	// in runChild) always names argv[0] exactly, never the resolved path.
	name := argv[0]
	if filepath.Base(name) != name || filepath.VolumeName(name) != "" {
		return "", executableCandidates(name, mergedEnvMap(request))
	}
	candidates := searchCandidates(name, mergedEnvMap(request))
	return "", candidates
}

// runChild walks the candidate list (in PATH order), relaying stdin and capturing
// stdout/stderr to the job's stream files, and returns the receipt's exit code and
// error text. Each candidate is tried in
// turn; an ENOENT candidate is skipped (nothing recorded); an EACCES or ENOEXEC
// candidate records the first such error and is skipped; the first candidate that
// actually executes wins. The child's merged environment is the parent environment
// updated with the request's environment.
func runChild(dir string, candidates []string, argv []string, cwd, stdin string, request map[string]any, facts map[string]string, sigCh <-chan os.Signal) (int, string) {
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
				var err error
				facts["environment"], err = executionEnvironmentDigest(request)
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

func executionEnvironmentDigest(request map[string]any) (string, error) {
	environment := map[string]any{}
	for key, value := range mergedEnvMap(request) {
		environment[key] = value
	}
	return CanonicalDigest(environment)
}

// tryExec attempts to start one PATH candidate. It returns ran=false when the
// exec did not happen (ENOENT, or a recorded EACCES/ENOEXEC error that should let
// the caller continue to the next candidate) and ran=true once a child has been
// started and reaped, in which case code/errorMsg are the receipt values.
func tryExec(candidate string, argv []string, cwd, stdin string, request map[string]any, outF, errF *os.File, sigCh <-chan os.Signal) (code int, errorMsg string, ran bool) {
	cmd := &exec.Cmd{
		Path:   candidate,
		Args:   argv,
		Dir:    cwd,
		Env:    mergedEnvList(request),
		Stdin:  bytes.NewReader([]byte(stdin)),
		Stdout: outF,
		Stderr: errF,
	}
	cmd.SysProcAttr = supervisorSysProcAttr()
	ApplyNativeCommandLine(cmd)
	settle, startErr := startChild(cmd, supervisedOwnedJob(request))
	if startErr != nil {
		var errno syscall.Errno
		if errors.As(startErr, &errno) && errno == syscall.ENOENT {
			// Nothing recorded for ENOENT; the caller continues the PATH walk.
			return 0, "", false
		}
		// EACCES, ENOEXEC, and any other errno: record the first and continue.
		return 0, mapExecError(startErr, argv[0]), false
	}
	if settle != nil {
		defer func() {
			if err := settle(); err != nil {
				code, errorMsg = 127, err.Error()
			}
		}()
	}
	code, errorMsg = waitChild(cmd, sigCh)
	return code, errorMsg, true
}

// waitChild reaps the started child and settles its receipt. The supervisor
// forwards cancellation to the isolated child and records 130 with the
// cancellation error. Independent child exits retain their exit or signal code.
func waitChild(cmd *exec.Cmd, sigCh <-chan os.Signal) (int, string) {
	type status struct {
		signaled bool
		sig      int
		exitCode int
		err      error
	}
	resCh := make(chan status, 1)
	go func() {
		waitErr := cmd.Wait()
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		var s status
		var exitErr *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exitErr) {
			s.err = waitErr
		}
		if ws.Signaled() {
			s.signaled = true
			s.sig = int(ws.Signal())
		} else {
			s.exitCode = ws.ExitStatus()
		}
		resCh <- s
	}()

	terminate := func() {
		signalSupervisedChild(cmd, false)
		select {
		case <-resCh:
		case <-time.After(5 * time.Second):
			signalSupervisedChild(cmd, true)
			<-resCh
		}
	}
	select {
	case <-sigCh:
		// Terminate the child and settle its exit before recording cancellation.
		terminate()
		return 130, "owned job was cancelled"
	case s := <-resCh:
		if s.err != nil {
			return 127, s.err.Error()
		}
		if s.signaled {
			return -s.sig, ""
		}
		return s.exitCode, ""
	}
}

// mergedEnvMap returns the parent environment updated with the request's
// environment. The PATH used for bare-name argv[0] resolution is read from this
// merged map, never from the parent process's PATH.
func mergedEnvMap(request map[string]any) map[string]string {
	merged := map[string]string{}
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i >= 0 {
			key := e[:i]
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			merged[key] = e[i+1:]
		}
	}
	if envRaw, ok := request["environment"].(map[string]any); ok {
		for k, v := range envRaw {
			if vs, ok := v.(string); ok {
				if runtime.GOOS == "windows" {
					k = strings.ToUpper(k)
				}
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
// is absent. Candidates are not pre-filtered by executability: runChild tries
// each candidate, recording the first EACCES/ENOEXEC and continuing, so the kernel's
// own errno — not a pre-flight Stat — decides which candidate runs. runChild
// performs that walk.
func searchCandidates(name string, env map[string]string) []string {
	pathEnv, ok := env["PATH"]
	if !ok {
		pathEnv = defpath
	}
	out := []string{}
	directories := filepath.SplitList(pathEnv)
	if pathEnv == "" {
		directories = []string{""}
	}
	for _, dir := range directories {
		var cand string
		if dir == "" {
			cand = "." + string(filepath.Separator) + name
		} else {
			cand = filepath.Join(dir, name)
		}
		out = append(out, executableCandidates(cand, env)...)
	}
	return out
}

func executableCandidates(name string, env map[string]string) []string {
	if runtime.GOOS != "windows" {
		return []string{name}
	}
	extensions := env["PATHEXT"]
	if extensions == "" {
		extensions = ".COM;.EXE;.BAT;.CMD"
	}
	candidates := []string{}
	if filepath.Ext(name) != "" {
		candidates = append(candidates, name)
	}
	for _, extension := range filepath.SplitList(extensions) {
		if extension != "" {
			if extension[0] != '.' {
				extension = "." + extension
			}
			candidates = append(candidates, name+extension)
		}
	}
	return candidates
}

func spawnErr(name string, errno int, filename string) string {
	msg := strerrorText[errno]
	if msg == "" {
		msg = "Unknown error"
	}
	return fmt.Sprintf("%s: [Errno %d] %s: '%s'", name, errno, msg, filename)
}

// mapExecError renders an exec start failure in the receipt's error format.
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

func proofToMap(p ContentProof) map[string]any {
	return map[string]any{"sha256": p.SHA256, "size": p.Size}
}

// writeAtomicJSON serialises value with the single canonical encoder and writes
// it atomically (temp file, fsync, rename). The value may be a typed record;
// canonicalBody routes it through its JSON tags into the canonical encoder, so
// the receipt uses the same canonical bytes without a second JSON encoding.
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

func supervisedOwnedJob(request map[string]any) bool {
	_, managed := request["engine"]
	return supervisedCheck(request) || request["kind"] == "native" && managed
}

// observeSupervisor reports liveness before cancelling a request-bound process group.
func observeSupervisor(directory, requestDigest string, cancel bool) (bool, error) {
	value, err := readJSONFile(filepath.Join(directory, "process.json"))
	if err != nil {
		return false, err
	}
	if textOf(getOf(value, "request_digest")) != requestDigest {
		return false, fmt.Errorf("supervisor process differs from its request")
	}
	alive := supervisorAlive(directory)
	if alive && cancel {
		pid, pgid := intOf(getOf(value, "pid")), intOf(getOf(value, "pgid"))
		actual, err := nativeProcessGroupID(pid)
		if err != nil || pgid <= 0 || actual != pgid {
			return false, fmt.Errorf("supervisor has no matching process group")
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			return false, err
		}
		if err := terminateJob(&exec.Cmd{Process: process}, pgid); err != nil {
			return false, err
		}
	}
	return alive, nil
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
