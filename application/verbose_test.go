package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/fradser/git-agent/application"
	"github.com/fradser/git-agent/domain/project"
)

func TestCommitService_Verbose_WritesDebugToLogWriter(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	var buf bytes.Buffer
	req := application.CommitRequest{
		Config:    &project.Config{},
		Verbose:   true,
		LogWriter: &buf,
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "staged files:") {
		t.Errorf("verbose output missing 'staged files:', got:\n%s", out)
	}
	if !strings.Contains(out, "diff lines:") {
		t.Errorf("verbose output missing 'diff lines:', got:\n%s", out)
	}
	if !strings.Contains(out, "calling LLM") {
		t.Errorf("verbose output missing 'calling LLM', got:\n%s", out)
	}
	if !strings.Contains(out, "LLM response received") {
		t.Errorf("verbose output missing 'LLM response received', got:\n%s", out)
	}
}

func TestCommitService_Verbose_False_NoOutput(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	var buf bytes.Buffer
	req := application.CommitRequest{
		Config:    &project.Config{},
		Verbose:   false,
		LogWriter: &buf,
	}
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() > 0 {
		t.Errorf("expected no verbose output when Verbose=false, got:\n%s", buf.String())
	}
}

func TestCommitService_Verbose_NilLogWriter_NoOutput(t *testing.T) {
	gen := &mockCommitGenerator{msg: defaultMsg()}
	git := &mockCommitGitClient{stagedDiff: defaultDiff()}
	svc := newSvc(gen, git, noopHook())

	req := application.CommitRequest{
		Config:    &project.Config{},
		Verbose:   true,
		LogWriter: nil,
	}
	// Should not panic when LogWriter is nil even if Verbose=true.
	if _, err := svc.Commit(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
