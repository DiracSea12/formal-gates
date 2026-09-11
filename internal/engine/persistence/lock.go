package persistence

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"formal-gates/internal/coordination"
)

// lockStaleAge 是锁的陈旧阈值，与 phase0 家族同值。engine 写事务是
// 毫秒级小事务，不需要 phase0 安装事务那样的长事务心跳（那里的心跳
// 是为大包 copy + smoke 级别的事务准备的）：持有进程死亡或锁文件
// ModTime 超过该阈值即可判定持有者失联、允许抢占，足以覆盖崩溃后
// 重启的解锁需求。
const lockStaleAge = 10 * time.Minute

var lockSequence atomic.Uint64

var transactionLocks = struct {
	sync.Mutex
	byDir map[string]*sync.Mutex
}{byDir: map[string]*sync.Mutex{}}

func transactionLock(dir string) *sync.Mutex {
	transactionLocks.Lock()
	defer transactionLocks.Unlock()
	if lock, ok := transactionLocks.byDir[dir]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	transactionLocks.byDir[dir] = lock
	return lock
}

// acquireLock 获取状态目录级独占写锁（master-requirements §2：同目录
// 并发写互斥）。锁是目录级而非单个文件级：目录内 state/intent/temp
// 的全部写入都在同一把锁下互斥。返回释放函数；释放即删除锁文件。
func (s *Store) acquireLock() (func(), error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	for {
		file, err := os.OpenFile(s.lockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			owner := fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), lockSequence.Add(1))
			if _, writeErr := file.WriteString(owner); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(s.lockPath())
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(s.lockPath())
				return nil, closeErr
			}
			return func() {
				data, readErr := readSharedFile(s.lockPath())
				if readErr == nil && string(data) == owner {
					_ = os.Remove(s.lockPath())
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// 锁被占用：只有陈旧锁（持有者已死或失联）允许抢占后重试，
		// 否则立即以 LockHeldError 拒绝，由调用方决定等待或上报。
		if lockIsStale(s.lockPath()) {
			if removeErr := os.Remove(s.lockPath()); removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
		}
		return nil, &LockHeldError{Path: s.lockPath()}
	}
}

// lockIsStale 判定锁是否可抢占：已完整登记的持有进程死亡，或锁已超过
// lockStaleAge。未完成写入的 owner 行必须等到超时，避免夺走正在创建的锁。
func lockIsStale(path string) bool {
	return lockIsStaleWithProcessProbe(path, coordination.ProcessAlive)
}

func lockIsStaleWithProcessProbe(path string, processAlive func(int) bool) bool {
	data, err := readSharedFile(path)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	expired := time.Since(info.ModTime()) > lockStaleAge
	// O_EXCL creates the visible file before WriteString publishes its owner.
	// Another process can observe empty or partial bytes in that interval. An
	// incomplete owner is recoverable only after the existing stale age, never
	// while a fresh creator may still be acquiring this transaction lock.
	if !strings.HasSuffix(string(data), "\n") {
		return expired
	}
	fields := strings.Fields(string(data))
	if (len(fields) != 1 && len(fields) != 2) || !strings.HasPrefix(fields[0], "pid=") {
		return expired
	}
	if len(fields) == 2 && !strings.HasPrefix(fields[1], "token=") {
		return expired
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(fields[0], "pid="))
	if err != nil || pid <= 0 {
		return expired
	}
	if !processAlive(pid) {
		// The observed owner may have released its lock and exited while we
		// queried its PID. Do not reclaim a subsequent writer's lock.
		current, err := readSharedFile(path)
		return err == nil && bytes.Equal(current, data)
	}
	return expired
}

func readSharedFile(path string) ([]byte, error) {
	file, err := openSharedFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
