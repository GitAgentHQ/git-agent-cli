package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestEnsureGitRepo_HonorsCanceledContextBeforeInit(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(dir, "init-ran")
	fakeGit := filepath.Join(bin, "git")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"rev-parse\" ]; then exit 1; fi\n" +
		"printf init > \"" + marker + "\"\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	var output bytes.Buffer
	cmd.SetOut(&output)

	if err := ensureGitRepo(cmd); err == nil {
		t.Fatal("expected canceled context to stop repository initialization")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("git init ran after cancellation; marker error=%v", err)
	}
}
