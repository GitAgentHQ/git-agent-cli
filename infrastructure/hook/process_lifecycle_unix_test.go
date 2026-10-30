//go:build !windows

package hook_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	domainHook "github.com/gitagenthq/git-agent/domain/hook"
	domainProject "github.com/gitagenthq/git-agent/domain/project"
	infraHook "github.com/gitagenthq/git-agent/infrastructure/hook"
)

func TestShellHook_CancellationTerminatesDescendants(t *testing.T) {
	// Allow process startup under concurrent build load while retaining a
	// bounded failure if the hook or its cancellation never completes.
	const processTimeout = 5 * time.Second
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "hook.sh")
	contents := "#!/bin/sh\n" +
		"(sleep 30) >/dev/null 2>&1 &\n" +
		"printf '%s' \"$!\" > \"" + marker + "\"\n" +
		"wait\n"
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		hook *domainHook.HookResult
		err  error
	}
	done := make(chan result, 1)
	go func() {
		hook, err := infraHook.NewShellHookExecutor().Execute(ctx, []string{script}, domainHook.HookInput{
			Config: domainProject.Config{},
		})
		done <- result{hook: hook, err: err}
	}()

	var pidText string
	deadline := time.Now().Add(processTimeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil {
			pidText = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pidText == "" {
		t.Fatal("hook did not start its child process")
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("invalid child pid %q: %v", pidText, err)
	}
	cancel()

	select {
	case <-time.After(processTimeout):
		t.Fatal("hook did not return after cancellation")
	case got := <-done:
		if got.err != nil {
			t.Fatalf("unexpected hook execution error: %v", got.err)
		}
		if got.hook == nil || got.hook.ExitCode == 0 {
			t.Fatalf("expected a non-zero hook result after cancellation, got %#v", got.hook)
		}
	}

	// A process can take a short moment to disappear after the group kill.
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			if err == syscall.ESRCH {
				return
			}
			t.Fatalf("checking child process %d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hook child process %d survived context cancellation", pid)
}
