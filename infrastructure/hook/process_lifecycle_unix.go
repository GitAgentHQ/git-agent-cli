//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package hook

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureHookProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func cancelHookProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Hooks are untrusted project code and may spawn formatters or other
	// helpers. Kill the process group on cancellation so those descendants do
	// not survive a canceled commit operation.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// The hook may have exited between cancellation and this group kill.
		// Report the same completion sentinel as os.Process.Kill so exec.Cmd
		// does not turn a successful exit into a spurious cancellation error.
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}
