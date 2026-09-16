// Package main implements the G0 SQLite driver compatibility probe.
//
// It verifies, in an isolated directory, the driver behaviours the formal-gates
// Go engine plan depends on *before* the driver version is locked: WAL
// journalling, foreign key enforcement, transaction semantics, the online
// backup API, WAL read/write concurrency and busy handling.
//
// This is development tooling. Nothing in the product imports it, and it does
// not participate in any run state.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	sqlitedriver "modernc.org/sqlite"
)

// DriverName is the sql.Register name used only by this probe, so registering
// cannot collide with a product driver.
const DriverName = "formal_gates_g0_sqlite_probe"

// backupConn is the unexported driver connection capability that exposes the
// SQLite online backup API. It is reached through RegisterConnectionHook.
type backupConn interface {
	NewBackup(dstURI string) (*sqlitedriver.Backup, error)
}

var (
	registerOnce sync.Once
	probeDriver  = &sqlitedriver.Driver{}
	connMu       sync.Mutex
	connByDSN    = map[string]backupConn{}
)

func ensureDriver() {
	registerOnce.Do(func() {
		probeDriver.RegisterConnectionHook(func(conn sqlitedriver.ExecQuerierContext, dsn string) error {
			if c, ok := conn.(backupConn); ok {
				connMu.Lock()
				connByDSN[dsn] = c
				connMu.Unlock()
			}
			return nil
		})
		sql.Register(DriverName, probeDriver)
	})
}

func connFor(dsn string) (backupConn, error) {
	connMu.Lock()
	defer connMu.Unlock()
	c, ok := connByDSN[dsn]
	if !ok {
		return nil, fmt.Errorf("no hooked connection observed for %s", dsn)
	}
	return c, nil
}

// CheckResult is one compatibility check.
type CheckResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Report is the machine-readable probe result.
type Report struct {
	Driver         string        `json:"driver"`
	DriverVersion  string        `json:"driver_version"`
	SQLiteVersion  string        `json:"sqlite_version"`
	BuiltWithCgo   bool          `json:"built_with_cgo"`
	GOOS           string        `json:"goos"`
	GOARCH         string        `json:"goarch"`
	CgoEnv         string        `json:"cgo_enabled_env"`
	Dir            string        `json:"dir"`
	Checks         []CheckResult `json:"checks"`
	Failed         []string      `json:"failed"`
	OK             bool          `json:"ok"`
	DurationMillis int64         `json:"duration_ms"`
}

// baseDSN builds a file URI with the pragmas every probe connection needs.
// The query separator matters: without "?" the pragma text becomes part of the
// file name and each variant would silently open a different database.
func baseDSN(path string, extras ...string) string {
	pragmas := []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
	}
	pragmas = append(pragmas, extras...)
	return "file:" + path + "?" + strings.Join(pragmas, "&")
}

// minimalDSN opens an already-WAL database without re-asserting journal_mode,
// so opening a second handle never has to negotiate the journal mode while
// another connection holds a write transaction.
func minimalDSN(path string, extras ...string) string {
	pragmas := []string{"_pragma=foreign_keys(1)"}
	pragmas = append(pragmas, extras...)
	return "file:" + path + "?" + strings.Join(pragmas, "&")
}

type probe struct {
	dir    string
	checks []CheckResult
}

func (p *probe) record(id, status, detail string, err error) {
	r := CheckResult{ID: id, Status: status, Detail: detail}
	if err != nil {
		r.Error = err.Error()
	}
	p.checks = append(p.checks, r)
}

func (p *probe) run(id string, fn func() (string, error)) {
	detail, err := fn()
	if err != nil {
		p.record(id, "FAIL", "", err)
		return
	}
	p.record(id, "PASS", detail, nil)
}

func abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// Run executes every check against an isolated database inside dir.
func Run(dir string) (*Report, error) {
	ensureDriver()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	start := time.Now()
	p := &probe{dir: dir}
	dbPath := filepath.Join(dir, "probe.sqlite")
	dsn := baseDSN(dbPath)

	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps the hooked connection alive and makes the
	// single-owner behaviour of the plan's runner explicit.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	var sqliteVersion string

	p.run("open-file-database", func() (string, error) {
		if err := db.Ping(); err != nil {
			return "", fmt.Errorf("ping: %w", err)
		}
		if err := db.QueryRow("SELECT sqlite_version()").Scan(&sqliteVersion); err != nil {
			return "", fmt.Errorf("sqlite_version: %w", err)
		}
		return fmt.Sprintf("opened %s, sqlite %s", abs(dbPath), sqliteVersion), nil
	})

	p.run("schema-and-pragmas", func() (string, error) {
		if _, err := db.Exec(`CREATE TABLE parent(id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id), note TEXT);`); err != nil {
			return "", err
		}
		var journal string
		var fk int64
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
			return "", err
		}
		if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
			return "", err
		}
		if !strings.EqualFold(journal, "wal") {
			return "", fmt.Errorf("journal_mode is %q, want wal", journal)
		}
		if fk != 1 {
			return "", fmt.Errorf("foreign_keys is %d, want 1", fk)
		}
		return fmt.Sprintf("journal_mode=%s foreign_keys=%d", journal, fk), nil
	})

	p.run("foreign-key-rejected", func() (string, error) {
		_, err := db.Exec("INSERT INTO child(id, parent_id, note) VALUES (1, 999, 'orphan')")
		if err == nil {
			return "", errors.New("orphan child row was accepted; foreign keys are not enforced")
		}
		if !strings.Contains(strings.ToUpper(err.Error()), "FOREIGN KEY") {
			return "", fmt.Errorf("unexpected error for orphan row: %w", err)
		}
		return "orphan child insert rejected: " + err.Error(), nil
	})

	p.run("transaction-commit-and-rollback", func() (string, error) {
		tx, err := db.Begin()
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec("INSERT INTO parent(id, name) VALUES (1, 'committed')"); err != nil {
			_ = tx.Rollback()
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		tx2, err := db.Begin()
		if err != nil {
			return "", err
		}
		if _, err := tx2.Exec("INSERT INTO parent(id, name) VALUES (2, 'rolled-back')"); err != nil {
			_ = tx2.Rollback()
			return "", err
		}
		if err := tx2.Rollback(); err != nil {
			return "", err
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM parent WHERE id = 1").Scan(&n); err != nil {
			return "", err
		}
		if n != 1 {
			return "", fmt.Errorf("committed row missing (count=%d)", n)
		}
		if err := db.QueryRow("SELECT count(*) FROM parent WHERE id = 2").Scan(&n); err != nil {
			return "", err
		}
		if n != 0 {
			return "", fmt.Errorf("rolled-back row survived (count=%d)", n)
		}
		return "commit persisted, rollback discarded", nil
	})

	p.run("wal-coordination-files", func() (string, error) {
		found := []string{}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); err == nil {
				found = append(found, filepath.Base(dbPath+suffix))
			}
		}
		if len(found) == 0 {
			return "", errors.New("no -wal/-shm coordination files observed on an active WAL database")
		}
		return "observed " + strings.Join(found, ", "), nil
	})

	p.run("online-backup-consistent", func() (string, error) {
		if _, err := db.Exec("INSERT INTO parent(id, name) VALUES (3, 'before-backup')"); err != nil {
			return "", err
		}
		conn, err := connFor(dsn)
		if err != nil {
			return "", err
		}
		copyPath := filepath.Join(p.dir, "backup.sqlite")
		_ = os.Remove(copyPath)
		b, err := conn.NewBackup("file:" + copyPath)
		if err != nil {
			return "", fmt.Errorf("new backup: %w", err)
		}
		for {
			more, err := b.Step(-1)
			if err != nil {
				_ = b.Finish()
				return "", fmt.Errorf("backup step: %w", err)
			}
			if !more {
				break
			}
		}
		if err := b.Finish(); err != nil {
			return "", fmt.Errorf("backup finish: %w", err)
		}
		// The copy must be a standalone database: no -wal dependency.
		restored, err := sql.Open(DriverName, minimalDSN(copyPath))
		if err != nil {
			return "", err
		}
		defer restored.Close()
		var name string
		if err := restored.QueryRow("SELECT name FROM parent WHERE id = 3").Scan(&name); err != nil {
			return "", fmt.Errorf("read backed-up row: %w", err)
		}
		if name != "before-backup" {
			return "", fmt.Errorf("backup content mismatch: %q", name)
		}
		integrity := ""
		if err := restored.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
			return "", err
		}
		if !strings.EqualFold(integrity, "ok") {
			return "", fmt.Errorf("backup integrity_check = %q", integrity)
		}
		info, err := os.Stat(copyPath)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("copy %s (%d bytes), integrity_check=ok", filepath.Base(copyPath), info.Size()), nil
	})

	p.run("wal-reader-not-blocked-by-open-writer", func() (string, error) {
		writer, err := sql.Open(DriverName, baseDSN(dbPath, "_txlock=immediate"))
		if err != nil {
			return "", err
		}
		defer writer.Close()
		writer.SetMaxOpenConns(1)
		tx, err := writer.Begin()
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec("INSERT INTO parent(id, name) VALUES (4, 'uncommitted')"); err != nil {
			_ = tx.Rollback()
			return "", err
		}
		var visible int
		if err := db.QueryRow("SELECT count(*) FROM parent WHERE id = 4").Scan(&visible); err != nil {
			_ = tx.Rollback()
			return "", fmt.Errorf("reader blocked by open writer: %w", err)
		}
		if err := tx.Rollback(); err != nil {
			return "", err
		}
		if visible != 0 {
			return "", fmt.Errorf("reader saw %d uncommitted rows", visible)
		}
		return "reader proceeded while a writer held an open transaction and saw no uncommitted rows", nil
	})

	p.run("busy-reported-then-resolved", func() (string, error) {
		holder, err := sql.Open(DriverName, baseDSN(dbPath, "_txlock=immediate"))
		if err != nil {
			return "", err
		}
		defer holder.Close()
		holder.SetMaxOpenConns(1)
		htx, err := holder.Begin()
		if err != nil {
			return "", err
		}
		if _, err := htx.Exec("INSERT INTO parent(id, name) VALUES (5, 'holder')"); err != nil {
			_ = htx.Rollback()
			return "", err
		}

		// A second handle on the same file, with no busy wait, must be told the
		// database is locked rather than silently proceeding.
		waiter, err := sql.Open(DriverName, minimalDSN(dbPath, "_pragma=busy_timeout(0)", "_txlock=immediate"))
		if err != nil {
			_ = htx.Rollback()
			return "", err
		}
		defer waiter.Close()
		waiter.SetMaxOpenConns(1)
		before := time.Now()
		waitTx, busyErr := waiter.Begin()
		elapsed := time.Since(before)
		if busyErr == nil {
			_ = waitTx.Rollback()
			_ = htx.Rollback()
			return "", errors.New("contending writer was not reported as busy while another write transaction was open")
		}
		if !strings.Contains(strings.ToLower(busyErr.Error()), "lock") && !strings.Contains(strings.ToLower(busyErr.Error()), "busy") {
			_ = htx.Rollback()
			return "", fmt.Errorf("contending writer failed with an unexpected error: %w", busyErr)
		}
		if err := htx.Rollback(); err != nil {
			return "", err
		}
		// Once the holder releases, the same handle must succeed.
		waitTx, err = waiter.Begin()
		if err != nil {
			return "", fmt.Errorf("writer still busy after the holder released: %w", err)
		}
		if _, err := waitTx.Exec("INSERT INTO parent(id, name) VALUES (6, 'after-release')"); err != nil {
			_ = waitTx.Rollback()
			return "", err
		}
		if err := waitTx.Rollback(); err != nil {
			return "", err
		}
		return fmt.Sprintf("contending writer reported busy after %s, then succeeded once released", elapsed.Round(time.Millisecond)), nil
	})

	failed := []string{}
	for _, c := range p.checks {
		if c.Status != "PASS" {
			failed = append(failed, c.ID)
		}
	}
	report := &Report{
		Driver:         "modernc.org/sqlite",
		DriverVersion:  "v1.36.1",
		SQLiteVersion:  sqliteVersion,
		BuiltWithCgo:   builtWithCgo,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		CgoEnv:         os.Getenv("CGO_ENABLED"),
		Dir:            abs(dir),
		Checks:         p.checks,
		Failed:         failed,
		OK:             len(failed) == 0,
		DurationMillis: time.Since(start).Milliseconds(),
	}
	return report, nil
}
