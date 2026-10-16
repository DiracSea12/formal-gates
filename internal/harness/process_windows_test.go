//go:build windows

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSupervisedWindowsPeer(t *testing.T) {
	switch os.Getenv("SUPERVISED_WINDOWS_PEER") {
	case "echo":
		fmt.Print(os.Getenv("SUPERVISED_WINDOWS_VALUE"))
		os.Exit(0)
	case "wait":
		if err := os.WriteFile(os.Getenv("SUPERVISED_WINDOWS_READY"), []byte("started"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
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
	bin := t.TempDir()
	if err := os.Link(executable, filepath.Join(bin, "peer.exe")); err != nil {
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
	command := exec.Command(supervisorBinary(t), "harness", "supervise", directory)
	command.SysProcAttr = supervisorSysProcAttr()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	waitForFile(t, ready)
	digest, err := CanonicalDigest(request)
	if err != nil {
		t.Fatal(err)
	}
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
