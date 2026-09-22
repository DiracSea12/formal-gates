package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSaveSubprocess 是跨进程 Save 测试的子进程入口，按
// PERSISTENCE_SAVE_ROLE 分派：
//
//   - abandon-holder：获取状态目录写锁后不释放直接退出，模拟持有者在
//     写事务中途异常终止（锁文件遗留、持有进程已死）；
//   - writer：对同一状态目录执行一次完整 Save（ExpectedRevision=0、
//     指纹恒定、内容携带自己的 writer 标记），把结果 JSON 写入
//     PERSISTENCE_SAVE_RESULT 文件。
func TestSaveSubprocess(t *testing.T) {
	dir := os.Getenv(saveRoleStateDirEnv)
	if dir == "" {
		t.Skip("subprocess entry only")
	}
	store, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatal(err)
	}
	switch os.Getenv(saveRoleEnv) {
	case "abandon-holder":
		if _, err := store.acquireLock(); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	case "writer":
		writer := os.Getenv(saveWriterEnv)
		result, err := store.Save(Transaction{
			ExpectedRevision:    0,
			ExpectedFingerprint: "fp",
			CollectFingerprint:  staticFingerprint("fp"),
			Content:             map[string]string{"writer": writer},
		})
		outcome := map[string]any{"writer": writer, "revision": result.Revision}
		switch {
		case err == nil:
			outcome["outcome"] = "committed"
		case errors.As(err, new(*LockHeldError)):
			outcome["outcome"] = "lock-held"
		case errors.As(err, new(*RevisionConflictError)):
			outcome["outcome"] = "revision-conflict"
		default:
			outcome["outcome"] = "unexpected"
			outcome["error"] = err.Error()
		}
		data, jsonErr := json.Marshal(outcome)
		if jsonErr != nil {
			t.Fatal(jsonErr)
		}
		if writeErr := os.WriteFile(os.Getenv(saveResultEnv), append(data, '\n'), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	default:
		t.Fatalf("unknown role %q", os.Getenv(saveRoleEnv))
	}
}

const (
	saveRoleEnv         = "PERSISTENCE_SAVE_ROLE"
	saveRoleStateDirEnv = "PERSISTENCE_SAVE_STATE_DIR"
	saveWriterEnv       = "PERSISTENCE_SAVE_WRITER"
	saveResultEnv       = "PERSISTENCE_SAVE_RESULT"
)

func runSaveSubprocess(dir, role, writer, resultPath string) (map[string]any, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSaveSubprocess$")
	cmd.Env = append(os.Environ(),
		saveRoleEnv+"="+role,
		saveRoleStateDirEnv+"="+dir,
		saveResultEnv+"="+resultPath,
		"GORACE=atexit_sleep_ms=0",
	)
	if writer != "" {
		cmd.Env = append(cmd.Env, saveWriterEnv+"="+writer)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("save subprocess %s: %w\n%s", role, err, out)
	}
	if role != "writer" {
		// abandon-holder 以持锁状态退出，没有可读的结果文件。
		return nil, nil
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("save subprocess %s result: %w", role, err)
	}
	var outcome map[string]any
	if err := json.Unmarshal(data, &outcome); err != nil {
		return nil, fmt.Errorf("save subprocess %s result %q: %w", role, data, err)
	}
	return outcome, nil
}

// TestSaveMutualExclusionAcrossProcesses：持有者异常终止后，两个独立进
// 程对同一状态目录并发提交同一期望 revision（ExpectedRevision=0、内容
// 不同）。不变量：至多一方成功提交、至少一方有确定结局；失败方必须是
// 锁拒绝（lock-held）或 CAS 拒绝（revision-conflict）这类明确失败，双
// 方同时成功或以其他错误失败都不算通过；最终状态只含成功方的写入，
// 不发生静默覆盖。
func TestSaveMutualExclusionAcrossProcesses(t *testing.T) {
	for round := 0; round < 150; round++ {
		dir := t.TempDir()
		results := filepath.Join(t.TempDir(), "results")
		if err := os.MkdirAll(results, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := runSaveSubprocess(dir, "abandon-holder", "", filepath.Join(results, "holder.json")); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		type writerResult struct {
			outcome map[string]any
			err     error
		}
		start := make(chan struct{})
		resultsCh := make(chan writerResult, 2)
		for _, writer := range []string{"0", "1"} {
			go func(writer string) {
				<-start
				outcome, err := runSaveSubprocess(dir, "writer", writer, filepath.Join(results, "writer-"+writer+".json"))
				resultsCh <- writerResult{outcome: outcome, err: err}
			}(writer)
		}
		close(start)
		first, second := <-resultsCh, <-resultsCh

		committed := 0
		committedWriter := ""
		for _, result := range []writerResult{first, second} {
			if result.err != nil {
				t.Fatalf("round %d: %v", round, result.err)
			}
			switch result.outcome["outcome"] {
			case "committed":
				committed++
				committedWriter = result.outcome["writer"].(string)
			case "lock-held", "revision-conflict":
			default:
				t.Fatalf("round %d: unexpected save outcome: %v", round, result.outcome)
			}
		}
		if committed > 1 {
			t.Fatalf("round %d: two processes both committed the same expected revision: %v %v", round, first.outcome, second.outcome)
		}
		if committed == 0 {
			t.Fatalf("round %d: no process reached a definite outcome: %v %v", round, first.outcome, second.outcome)
		}

		store, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
		if err != nil {
			t.Fatal(err)
		}
		state, err := store.Load()
		if err != nil {
			t.Fatalf("round %d: load: %v", round, err)
		}
		if state.Revision != 1 {
			t.Fatalf("round %d: final revision = %d, want 1", round, state.Revision)
		}
		var content struct {
			Writer string `json:"writer"`
		}
		if err := json.Unmarshal(state.Content, &content); err != nil {
			t.Fatalf("round %d: content: %v", round, err)
		}
		if content.Writer != committedWriter {
			t.Fatalf("round %d: final content writer = %q, want the sole committer %q", round, content.Writer, committedWriter)
		}
	}
}
