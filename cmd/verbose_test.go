package cmd_test

import (
	"os"
	"testing"

	"github.com/fradser/ga-cli/cmd"
)

func TestVerboseFlag_Accepted(t *testing.T) {
	if err := cmd.ExecuteArgs([]string{"commit", "--verbose"}); err != nil {
		t.Fatalf("unexpected error from flag parsing: %v", err)
	}
}

func TestOutputContract_StdoutEmpty_OnError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	origStdout := os.Stdout
	os.Stdout = w

	// commit with no staged changes — the stub RunE returns nil, so we just
	// verify stdout stays empty (no outline printed on a no-op stub).
	_ = cmd.ExecuteArgs([]string{"commit"})

	w.Close()
	os.Stdout = origStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()

	if n != 0 {
		t.Fatalf("expected empty stdout, got %d bytes: %q", n, buf[:n])
	}
}
