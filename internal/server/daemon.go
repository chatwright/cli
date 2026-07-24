// daemon.go implements the process lifecycle behind `chatwright server
// start`/`stop`: writing and reading a PID file, detecting a stale one (the
// recorded process no longer exists), re-executing this same binary as a
// detached `server serve` child, and terminating it again. The platform-
// specific primitives it needs — "is this pid still alive", "ask it to
// terminate", and "what SysProcAttr detaches a child from this process's
// session" — live in daemon_unix.go/daemon_windows.go behind three small
// functions, so everything else here is pure, portable Go and directly
// testable without actually forking a long-lived daemon.
package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrAlreadyRunning is returned by Start when the PID file names a process
// that is still alive.
var ErrAlreadyRunning = errors.New("chatwright server: already running")

// ErrNotRunning is returned by Stop when there is no PID file, or the PID
// file names a process that is no longer alive (a stale PID file, which
// Stop cleans up before returning this error).
var ErrNotRunning = errors.New("chatwright server: not running")

// defaultStopWait bounds how long Stop waits for a SIGTERM'd process to
// exit before giving up (it still removes the PID file either way — a
// process that ignores SIGTERM for this long is not coming back from this
// call, and leaving a stale-looking PID file around only confuses the next
// start/stop).
const defaultStopWait = 5 * time.Second

// WritePIDFile writes pid to path as decimal text plus a trailing newline.
func WritePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

// ReadPIDFile reads and parses the PID previously written by WritePIDFile.
// A missing file is reported via the underlying os.IsNotExist-satisfying
// error, unwrapped, so callers can distinguish "no PID file" from "PID file
// exists but is corrupt."
func ReadPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("pid file %s: invalid content %q: %w", path, text, err)
	}
	return pid, nil
}

// RemovePIDFile removes path, treating an already-missing file as success.
func RemovePIDFile(path string) error {
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// IsProcessRunning reports whether pid names a live process. It is exported
// so `chatwright server` reports accurate status without duplicating this
// package's platform-specific logic.
func IsProcessRunning(pid int) bool { return isProcessRunning(pid) }

// StartOptions configures Start.
type StartOptions struct {
	// Executable is the binary to re-exec — the running binary's own path
	// (os.Executable()), so `start` always launches the same build of
	// chatwright that is running the `start` command itself.
	Executable string
	// Args are the arguments to launch Executable with — the caller's job
	// is to pass e.g. ["server", "serve", "--addr", addr, ...] so the
	// child runs `serve` in the foreground of its own detached session.
	Args []string
	// PIDFile is where the spawned child's PID is recorded.
	PIDFile string
	// LogFile receives the child's stdout and stderr.
	LogFile string
}

// Start launches a detached child per StartOptions and records its PID.
// A PID file naming a still-live process is treated as ErrAlreadyRunning
// without touching anything; a PID file naming a dead process (stale) is
// removed and Start proceeds normally — this is the "stale PID file
// detection" the daemon lifecycle needs, factored out here so it is
// exercised by tests without any real daemonizing.
func Start(opts StartOptions) (pid int, err error) {
	if existing, readErr := ReadPIDFile(opts.PIDFile); readErr == nil {
		if isProcessRunning(existing) {
			return 0, fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, existing)
		}
		// Stale: the recorded process is gone. Clear it and proceed.
		if err := RemovePIDFile(opts.PIDFile); err != nil {
			return 0, fmt.Errorf("removing stale pid file: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		// The PID file exists but could not be read/parsed — surface this
		// rather than silently overwriting a file we do not understand.
		return 0, fmt.Errorf("reading pid file: %w", readErr)
	}

	logFile, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}
	defer func() { _ = logFile.Close() }() // safe post-Start: the child inherits its own duplicated fd

	cmd := exec.Command(opts.Executable, opts.Args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = daemonSysProcAttr()

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting daemon: %w", err)
	}
	childPID := cmd.Process.Pid

	if err := WritePIDFile(opts.PIDFile, childPID); err != nil {
		_ = cmd.Process.Kill() // don't leave an unmanaged, unrecorded orphan running
		return 0, fmt.Errorf("writing pid file: %w", err)
	}
	// Detach: this process no longer needs (and, being a new session
	// leader via daemonSysProcAttr, should not keep) a handle on the child.
	_ = cmd.Process.Release()

	return childPID, nil
}

// Stop reads pidPath, sends the platform's termination signal to the
// process it names, waits up to wait for it to exit, and removes pidPath.
// It returns ErrNotRunning (after cleaning up a stale PID file, if that is
// what was found) when there was nothing to stop. wait <= 0 uses
// defaultStopWait.
func Stop(pidPath string, wait time.Duration) error {
	if wait <= 0 {
		wait = defaultStopWait
	}

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotRunning
		}
		return fmt.Errorf("reading pid file: %w", err)
	}

	if !isProcessRunning(pid) {
		_ = RemovePIDFile(pidPath)
		return ErrNotRunning
	}

	if err := terminate(pid); err != nil {
		return fmt.Errorf("signaling pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !isProcessRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return RemovePIDFile(pidPath)
}
