package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// implausiblePID is a pid value used to simulate "this PID file is stale
// (the process it names is gone)" without depending on real OS process-
// table timing: a genuinely-exited real pid can be reused by the kernel
// moments later on a busy machine (this bit us in practice — see git
// history), making "spawn, wait, then assert this same pid is not running"
// flaky. 999999999 exceeds every real OS's documented pid_max by a wide
// margin (Linux's hard cap is 2^22 = 4,194,304; Darwin's traditional
// PID_MAX is 99999), so a liveness probe against it is deterministically
// "not running" everywhere these tests run, with no possibility of
// collision.
const implausiblePID = 999999999

func TestPIDFileWriteReadRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.pid")

	if _, err := ReadPIDFile(path); !os.IsNotExist(err) {
		t.Fatalf("ReadPIDFile() on a missing file = %v, want an os.IsNotExist error", err)
	}

	if err := WritePIDFile(path, 4242); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4242 {
		t.Fatalf("ReadPIDFile() = %d, want 4242", got)
	}

	if err := RemovePIDFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("pid file still exists after RemovePIDFile")
	}
	// Removing an already-absent file is not an error.
	if err := RemovePIDFile(path); err != nil {
		t.Fatalf("RemovePIDFile() on an already-missing file = %v, want nil", err)
	}
}

func TestReadPIDFileRejectsCorruptContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.pid")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPIDFile(path); err == nil {
		t.Fatal("ReadPIDFile() on corrupt content = nil error, want an error")
	}
}

func TestIsProcessRunningTrueForSelf(t *testing.T) {
	if !IsProcessRunning(os.Getpid()) {
		t.Fatal("IsProcessRunning(self) = false, want true")
	}
}

func TestIsProcessRunningFalseForAnImplausiblePID(t *testing.T) {
	if IsProcessRunning(implausiblePID) {
		t.Fatalf("IsProcessRunning(%d) = true, want false (no such pid can exist)", implausiblePID)
	}
}

func TestStartDetectsStalePIDFileAndProceeds(t *testing.T) {
	requireSleepBinary(t)

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")
	logPath := filepath.Join(dir, "server.log")

	// A PID file naming a pid that cannot possibly be running is stale.
	if err := WritePIDFile(pidPath, implausiblePID); err != nil {
		t.Fatal(err)
	}

	pid, err := Start(StartOptions{
		Executable: "sleep",
		Args:       []string{"5"},
		PIDFile:    pidPath,
		LogFile:    logPath,
	})
	if err != nil {
		t.Fatalf("Start() over a stale pid file = %v, want it to proceed", err)
	}
	t.Cleanup(func() { _ = terminate(pid) })
	reapInBackground(t, pid)

	if !IsProcessRunning(pid) {
		t.Fatal("Start() reported a pid that is not actually running")
	}
	gotPID, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPID != pid {
		t.Fatalf("pid file = %d, want %d", gotPID, pid)
	}
}

func TestStartRejectsWhenAlreadyRunning(t *testing.T) {
	requireSleepBinary(t)

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")
	logPath := filepath.Join(dir, "server.log")

	pid, err := Start(StartOptions{
		Executable: "sleep",
		Args:       []string{"5"},
		PIDFile:    pidPath,
		LogFile:    logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminate(pid) })
	reapInBackground(t, pid)

	_, err = Start(StartOptions{
		Executable: "sleep",
		Args:       []string{"5"},
		PIDFile:    pidPath,
		LogFile:    logPath,
	})
	if err == nil {
		t.Fatal("second Start() over a live pid file = nil error, want ErrAlreadyRunning")
	}
}

func TestStopTerminatesAndCleansUpPIDFile(t *testing.T) {
	requireSleepBinary(t)

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")
	logPath := filepath.Join(dir, "server.log")

	pid, err := Start(StartOptions{
		Executable: "sleep",
		Args:       []string{"30"},
		PIDFile:    pidPath,
		LogFile:    logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Reap in the background: this test process is the started child's
	// real parent (unlike production, where the short-lived `start`
	// process exits and the daemon is reparented to init, which reaps it
	// for free) — without this, a terminated child sits as a zombie that
	// still answers a liveness probe, which would make this test's own
	// assertion below flaky rather than the production code wrong.
	reapInBackground(t, pid)
	if !IsProcessRunning(pid) {
		t.Fatal("started process is not running")
	}

	if err := Stop(pidPath, 3*time.Second); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if IsProcessRunning(pid) {
		t.Fatal("process still running after Stop()")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("pid file still exists after Stop()")
	}
}

func TestStopOnMissingPIDFileReturnsErrNotRunning(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "server.pid")
	if err := Stop(pidPath, 0); err != ErrNotRunning {
		t.Fatalf("Stop() on a missing pid file = %v, want ErrNotRunning", err)
	}
}

func TestStopOnStalePIDFileCleansUpAndReturnsErrNotRunning(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "server.pid")

	if err := WritePIDFile(pidPath, implausiblePID); err != nil {
		t.Fatal(err)
	}

	if err := Stop(pidPath, 0); err != ErrNotRunning {
		t.Fatalf("Stop() on a stale pid file = %v, want ErrNotRunning", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("stale pid file was not cleaned up by Stop()")
	}
}

// requireSleepBinary skips a test on a platform without a "sleep" binary on
// PATH (there is no portable Go stand-in for "a long-lived external
// process to daemonize" without actually re-invoking this test binary in a
// mode that blocks, which would be far more fragile than skipping).
func requireSleepBinary(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no 'sleep' binary on windows")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("'sleep' binary not found on PATH")
	}
}

// reapInBackground waits for pid — a real child this test process started
// directly — in a background goroutine, so a process this test terminates
// does not linger as a zombie for the rest of the test binary's run (a
// zombie still answers a liveness probe, per this file's package doc
// comment on TestStopTerminatesAndCleansUpPIDFile).
func reapInBackground(t *testing.T, pid int) {
	t.Helper()
	go func() {
		if p, err := os.FindProcess(pid); err == nil {
			_, _ = p.Wait()
		}
	}()
}
