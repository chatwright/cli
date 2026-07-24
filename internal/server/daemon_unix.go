//go:build !windows

package server

import (
	"errors"
	"os"
	"syscall"
)

// daemonSysProcAttr detaches the started child into its own session (a new
// process group with no controlling terminal), so it survives the parent
// `chatwright server start` process exiting and is not killed by, e.g., the
// parent's terminal closing.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// isProcessRunning reports whether pid is alive by sending it signal 0 —
// the standard Unix "does this process exist and am I allowed to signal
// it" probe, which delivers no actual signal. A permission error still
// means the process exists (it belongs to another user); only "no such
// process" means it does not.
//
// Go's os.Process.Signal translates the underlying ESRCH errno into the
// sentinel os.ErrProcessDone rather than surfacing a bare syscall.Errno —
// confirmed against this repository's own Go toolchain, not assumed — so
// that is the primary check; a raw syscall.ESRCH is also accepted in case
// a future/other platform surfaces it unwrapped instead.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
		return false
	}
	// Any other error (e.g. EPERM) still means the process exists.
	return true
}

// terminate asks pid to shut down gracefully via SIGTERM — the signal
// net/http.Server's own graceful-shutdown trap (see cmd/chatwright's
// server.go) listens for. A process that has already exited between
// Stop's own isProcessRunning check and this call is treated as success
// (nothing left to terminate), not an error — the same
// os.ErrProcessDone translation isProcessRunning relies on.
func terminate(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	err = process.Signal(syscall.SIGTERM)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
