//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package hook

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestCancelHookProcessTreatsExitedProcessAsDone(t *testing.T) {
	cmd := exec.Command("true")
	configureHookProcess(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("run command: %v", err)
	}

	err := cancelHookProcess(cmd)
	if !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("expected os.ErrProcessDone for exited process, got %v (ESRCH=%v)", err, syscall.ESRCH)
	}
}
