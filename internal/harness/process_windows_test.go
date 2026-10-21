//go:build windows

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"

	"golang.org/x/sys/windows"
)

func TestSupervisedWindowsPeer(t *testing.T) {
	switch os.Getenv("SUPERVISED_WINDOWS_PEER") {
	case "echo":
		fmt.Print(os.Getenv("SUPERVISED_WINDOWS_VALUE"))
		os.Exit(0)
	case "argv":
		if len(os.Args) < 3 || os.Args[2] != "--" {
			t.Fatal("expected original peer argument boundary", os.Args)
		}
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"argv": os.Args[3:], "stdin": string(body)}); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	case "native-claim":
		directory := os.Getenv("SUPERVISED_WINDOWS_DIRECTORY")
		request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := prepareNativeInvocation(directory, objectOrEmpty(request), nativeImplementation{}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("SUPERVISED_WINDOWS_READY"), []byte("claimed"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
	case "wait":
		if err := os.WriteFile(os.Getenv("SUPERVISED_WINDOWS_READY"), []byte("started"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
	}
}

func TestSuperviseWindowsPreservesQuotedShellTextAndNativeArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(t.TempDir(), "原文 quoted peer.exe")
	if err := os.WriteFile(peer, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	input := "first \"原文\"\r\nsecond\t  "
	arguments := []string{"", "two words", `原文 "quoted"`}
	native := append([]string{peer, "-test.run=^TestSupervisedWindowsPeer$", "--"}, arguments...)
	text := `"` + peer + `" -test.run=^TestSupervisedWindowsPeer$ -- "" "two words" "原文 \"quoted\""`
	for name, argv := range map[string][]string{"native": native, "shell": coverage.ShellCommand(text)} {
		t.Run(name, func(t *testing.T) {
			originalArgv := append([]string(nil), argv...)
			command := exec.Command(argv[0], argv[1:]...)
			command.SysProcAttr = supervisorSysProcAttr()
			attributes := *command.SysProcAttr
			ApplyNativeCommandLine(command)
			if !reflect.DeepEqual(command.Args, originalArgv) || command.SysProcAttr.CreationFlags != attributes.CreationFlags || name == "native" && command.SysProcAttr.CmdLine != "" {
				t.Fatal("native command changed arguments or process-group attributes", command)
			}
			directory := t.TempDir()
			request := map[string]any{"kind": "check", "argv": argv, "cwd": directory, "stdin": input, "environment": map[string]any{"SUPERVISED_WINDOWS_PEER": "argv"}}
			writeRequest(t, directory, request)
			originalRequest := readBytes(t, filepath.Join(directory, "request.json"))
			if code := Supervise(directory); code != 0 {
				t.Fatal("supervisor failed", code)
			}
			receipt := readReceipt(t, directory)
			if receipt["exit_code"] != float64(0) || receipt["error"] != "" {
				t.Fatal("original command failed", receipt, string(readBytes(t, filepath.Join(directory, "stderr.log"))))
			}
			var observed struct {
				Argv  []string `json:"argv"`
				Stdin string   `json:"stdin"`
			}
			if err := json.Unmarshal(readBytes(t, filepath.Join(directory, "stdout.log")), &observed); err != nil || !reflect.DeepEqual(observed.Argv, arguments) || observed.Stdin != input {
				t.Fatal("original native arguments or stdin changed", observed, err)
			}
			if !bytes.Equal(originalRequest, readBytes(t, filepath.Join(directory, "request.json"))) {
				t.Fatal("native launch rewrote the original approved request")
			}
		})
	}
	ordinary := exec.Command(peer, "/D", "/S", "/C", text)
	ApplyNativeCommandLine(ordinary)
	if ordinary.SysProcAttr != nil || !reflect.DeepEqual(ordinary.Args, []string{peer, "/D", "/S", "/C", text}) {
		t.Fatal("ordinary ARGV acquired CMD parsing", ordinary)
	}
}

func TestSupervisorWindowsIdentityUsesOperatingSystemCreationTime(t *testing.T) {
	created, ok := processCreationTime(os.Getpid())
	if !ok {
		t.Fatal("cannot read current process creation time")
	}
	directory := t.TempDir()
	if err := claimJob(directory, map[string]any{"kind": "check"}); err != nil {
		t.Fatal(err)
	}
	value, err := readJSONFile(filepath.Join(directory, "process.json"))
	if err != nil || floatOf(getOf(value, "started_at")) != float64(created.UnixNano())/1e9 || !supervisorAlive(directory) {
		t.Fatal("live original process identity was not preserved", value, err)
	}
	value.(map[string]any)["started_at"] = float64(created.Add(time.Minute).UnixNano()) / 1e9
	if err := writeAtomicJSON(filepath.Join(directory, "process.json"), value); err != nil {
		t.Fatal(err)
	}
	if supervisorAlive(directory) {
		t.Fatal("different process creation identity was treated as the original")
	}
}

func TestSuperviseWindowsUsesRequestPathAndExecutableExtensions(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "peer.exe"), payload, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATHEXT", ".missing")
	for _, name := range []string{"peer", filepath.Join(bin, "peer")} {
		directory := t.TempDir()
		request := map[string]any{"kind": "check", "argv": []string{name, "-test.run=^TestSupervisedWindowsPeer$"}, "cwd": directory, "environment": map[string]any{"Path": filepath.Join(t.TempDir(), "missing") + string(os.PathListSeparator) + bin, "PATHEXT": ".EXE", "SUPERVISED_WINDOWS_PEER": "echo", "SUPERVISED_WINDOWS_VALUE": name}}
		writeRequest(t, directory, request)
		if code := Supervise(directory); code != 0 {
			t.Fatal("supervisor failed", code)
		}
		receipt := readReceipt(t, directory)
		if receipt["exit_code"] != float64(0) || receipt["error"] != "" || string(readBytes(t, filepath.Join(directory, "stdout.log"))) != name {
			t.Fatal("native request PATH, extension or input changed", receipt)
		}
	}
}

func TestSuperviseWindowsCancellationSettlesOriginalReceipt(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	ready := filepath.Join(directory, "ready")
	request := map[string]any{"kind": "approved-development-check", "argv": []string{executable, "-test.run=^TestSupervisedWindowsPeer$"}, "cwd": directory, "environment": map[string]any{"SUPERVISED_WINDOWS_PEER": "wait", "SUPERVISED_WINDOWS_READY": ready}}
	writeRequest(t, directory, request)
	digest, err := digestOf(t, request)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(supervisorBinary(t), "harness", "supervise", directory)
	command.SysProcAttr = supervisorSysProcAttr()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	waitForFile(t, ready)
	if alive, err := observeSupervisor(directory, digest, true); err != nil || !alive {
		t.Fatal("original live supervisor could not receive cancellation", alive, err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt := readReceipt(t, directory)
	if receipt["exit_code"] != float64(130) || receipt["error"] != "owned job was cancelled" || receipt["request_digest"] != digest {
		t.Fatal("original cancellation receipt changed", receipt)
	}
}

func TestSuperviseWindowsSettlesOwnedDescendants(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"completed", "cancelled", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			eventName := fmt.Sprintf(`Local\formal-gates-%d-%s`, os.Getpid(), strings.ReplaceAll(t.Name(), "/", "-"))
			name, err := windows.UTF16PtrFromString(eventName)
			if err != nil {
				t.Fatal(err)
			}
			event, err := windows.CreateEvent(nil, 1, 0, name)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(event)
			ready, pidfile := filepath.Join(directory, "ready"), filepath.Join(directory, "child.pid")
			script := `$child=[Diagnostics.ProcessStartInfo]::new($env:SUPERVISED_WINDOWS_EXECUTABLE,'-test.run=^TestSupervisedWindowsPeer$'); $child.UseShellExecute=$false; $process=[Diagnostics.Process]::Start($child); [Console]::Error.Write('original diagnostic'); $pidfile=$env:SUPERVISED_WINDOWS_PID; [IO.File]::WriteAllText($pidfile+'.tmp',[string]$process.Id); [IO.File]::Move($pidfile+'.tmp',$pidfile); $gate=[Threading.EventWaitHandle]::OpenExisting($env:SUPERVISED_WINDOWS_EVENT); $null=$gate.WaitOne(); [Console]::Write('completed')`
			request := map[string]any{
				"kind": "approved-development-check", "argv": coverage.ShellCommand(`powershell -NoProfile -NonInteractive -Command "` + script + `"`), "cwd": directory,
				"environment": map[string]any{"SUPERVISED_WINDOWS_EXECUTABLE": executable, "SUPERVISED_WINDOWS_PEER": "wait", "SUPERVISED_WINDOWS_READY": ready, "SUPERVISED_WINDOWS_PID": pidfile, "SUPERVISED_WINDOWS_EVENT": eventName},
			}
			if mode == "ordinary" {
				request["kind"] = "check"
			}
			writeRequest(t, directory, request)
			original := readBytes(t, filepath.Join(directory, "request.json"))
			digest, err := digestOf(t, request)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(supervisorBinary(t), "harness", "supervise", directory)
			command.SysProcAttr = supervisorSysProcAttr()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			waitForFile(t, ready)
			waitForFile(t, pidfile)
			pid, err := strconv.Atoi(string(readBytes(t, pidfile)))
			if err != nil {
				t.Fatal(err)
			}
			child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = windows.TerminateProcess(child, 1)
				_, _ = windows.WaitForSingleObject(child, windows.INFINITE)
				_ = windows.CloseHandle(child)
			})
			if mode == "cancelled" {
				if err := terminateJob(command, command.Process.Pid); err != nil {
					t.Fatal(err)
				}
			} else if err := windows.SetEvent(event); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			receipt := readReceipt(t, directory)
			code, errorText := float64(0), ""
			if mode == "cancelled" {
				code, errorText = 130, "owned job was cancelled"
			}
			if receipt["exit_code"] != code || receipt["error"] != errorText || receipt["request_digest"] != digest || !bytes.Equal(original, readBytes(t, filepath.Join(directory, "request.json"))) {
				t.Fatal("original command or receipt changed", receipt)
			}
			streams := receipt["streams"].(map[string]any)
			for _, stream := range []string{"stdout.log", "stderr.log"} {
				proof, refusal := FileProof(filepath.Join(directory, stream))
				recorded := streams[stream].(map[string]any)
				if refusal != "" || proof.SHA256 != recorded["sha256"] || float64(proof.Size) != recorded["size"] {
					t.Fatal("original stream proof changed", stream, proof, streams[stream], refusal)
				}
			}
			if diagnostic := string(readBytes(t, filepath.Join(directory, "stderr.log"))); diagnostic != "original diagnostic" {
				t.Fatalf("original diagnostic changed: %q", diagnostic)
			}
			output := "completed"
			if mode == "cancelled" {
				output = ""
			}
			if actual := string(readBytes(t, filepath.Join(directory, "stdout.log"))); actual != output {
				t.Fatalf("original output changed: %q want %q", actual, output)
			}
			status, err := windows.WaitForSingleObject(child, 0)
			expected := uint32(windows.WAIT_OBJECT_0)
			if mode == "ordinary" {
				expected = uint32(windows.WAIT_TIMEOUT)
			}
			if err != nil || status != expected {
				t.Fatalf("descendant %d state=%d want=%d: %v", pid, status, expected, err)
			}
			if mode != "ordinary" {
				for _, stream := range []string{"stdout.log", "stderr.log"} {
					if err := os.Remove(filepath.Join(directory, stream)); err != nil {
						t.Fatal("completed owned stream still has a process holder", err)
					}
				}
			}
		})
	}
}

func TestNativeJobStoppedRequiresOriginalClaimAndStoppedChild(t *testing.T) {
	directory := t.TempDir()
	if NativeJobStopped(directory) {
		t.Fatal("missing claim counted as stopped")
	}
	ready := filepath.Join(directory, "ready")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestSupervisedWindowsPeer$")
	cmd.Env = append(os.Environ(), "SUPERVISED_WINDOWS_PEER=wait", "SUPERVISED_WINDOWS_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("peer not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	created, ok := processCreationTime(cmd.Process.Pid)
	if !ok {
		t.Fatal("process creation unavailable")
	}
	request := map[string]any{"kind": "native", "engine": map[string]any{"action_id": "original"}}
	digest, err := CanonicalDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	process := map[string]any{"pid": cmd.Process.Pid, "pgid": cmd.Process.Pid, "started_at": float64(created.UnixNano()) / 1e9, "request_digest": digest}
	for name, value := range map[string]any{"request.json": request, "process.json": process} {
		if err := writeAtomicJSON(filepath.Join(directory, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if NativeJobStopped(directory) {
		t.Fatal("live original process counted as stopped")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if !NativeJobStopped(directory) {
		t.Fatal("dead claimed process not observed")
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-process.json"), map[string]any{"pid": os.Getpid(), "pgid": cmd.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	if NativeJobStopped(directory) {
		t.Fatal("live recorded child counted as stopped")
	}
	if err := os.Remove(filepath.Join(directory, "native-process.json")); err != nil {
		t.Fatal(err)
	}
	process["request_digest"] = "different"
	if err := writeAtomicJSON(filepath.Join(directory, "process.json"), process); err != nil {
		t.Fatal(err)
	}
	if NativeJobStopped(directory) {
		t.Fatal("another request's process counted as stopped")
	}
}

func TestNativeDirectClaimDoesNotBorrowSupervisorStopProof(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "native")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engine := map[string]any{"workerMaterial": map[string]any{"actionId": "original", "attemptId": "attempt"}}
	native := map[string]any{"kind": "native", "provider": "zcode", "engine": engine}
	outer := map[string]any{"kind": "native", "argv": []string{executable, "harness", "native", directory}, "engine": engine}
	for name, value := range map[string]any{"native-request.json": native, "request.json": outer} {
		if err := writeAtomicJSON(filepath.Join(directory, name), value); err != nil {
			t.Fatal(err)
		}
	}
	ready := filepath.Join(directory, "ready")
	cmd := exec.Command(executable, "-test.run=^TestSupervisedWindowsPeer$")
	cmd.Env = append(os.Environ(), "SUPERVISED_WINDOWS_PEER=native-claim", "SUPERVISED_WINDOWS_DIRECTORY="+directory, "SUPERVISED_WINDOWS_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct native claim not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	process, err := readJSONFile(filepath.Join(directory, "process.json"))
	if err != nil {
		t.Fatal(err)
	}
	nativeDigest, err := CanonicalDigest(native)
	if err != nil {
		t.Fatal(err)
	}
	outerRecorded, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	outerDigest, err := CanonicalDigest(outerRecorded)
	if err != nil {
		t.Fatal(err)
	}
	if intOf(getOf(process, "pid")) != cmd.Process.Pid || getOf(process, "request_digest") != nativeDigest || nativeDigest == outerDigest {
		t.Fatal("direct claim did not retain original native request binding", process)
	}
	if !NativeJobAlive(directory) {
		t.Fatal("direct owner not observed alive")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if NativeJobAlive(directory) {
		t.Fatal("dead direct owner reported alive")
	}
	if _, err := os.Stat(filepath.Join(directory, "native-process.json")); !os.IsNotExist(err) {
		t.Fatal("fixture unexpectedly has provider process proof", err)
	}
	if NativeJobStopped(directory) {
		t.Fatal("direct owner death borrowed unrelated outer supervisor job guarantee")
	}
}
