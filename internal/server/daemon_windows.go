//go:build windows

package server

import (
	"os"
	"syscall"
)

// daemonSysProcAttr detaches the started child from the parent's console
// session so a later console close does not also close the child.
// Windows has no equivalent of Unix's SIGTERM-based graceful shutdown for
// an arbitrary process, so Stop's terminate below falls back to a hard
// Kill on this platform — a documented limitation, not a gap this CLI
// papers over.
func daemonSysProcAttr() *syscall.SysProcAttr {
	const createNewProcessGroup = 0x00000200
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// isProcessRunning is best-effort on Windows: os.FindProcess opens a
// handle to pid, which fails once the process has exited.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// A zero-signal probe is not supported on Windows; Wait would block on
	// a process we don't own, so treat "we could open a handle" as
	// running. This can be wrong for a just-exited, not-yet-reaped pid —
	// see this file's doc comment.
	_ = process
	return true
}

// terminate hard-kills pid: Windows processes do not have a portable
// graceful-shutdown signal equivalent to SIGTERM, so `chatwright server
// stop` cannot ask this server to drain in-flight requests on Windows the
// way it can on Unix — see daemonSysProcAttr's doc comment.
func terminate(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
