package application_test

import (
	"testing"

	"github.com/gitagenthq/git-agent/application"
)

func TestClassifyCommand(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		wantTest bool
		wantBld  bool
		wantName string
	}{
		{"go test all", "go test ./...", true, false, ""},
		{
			"go test with run",
			"go test ./application/... -run TestCommitService_NoStagedChanges",
			true, false, "TestCommitService_NoStagedChanges",
		},
		{"make test", "make test", true, false, ""},
		{"pnpm test", "pnpm test", true, false, ""},
		{"pytest", "pytest tests/", true, false, ""},
		{"cargo test", "cargo test", true, false, ""},
		{"go build", "go build ./...", false, true, ""},
		{"make build", "make build", false, true, ""},
		{"ls", "ls", false, false, ""},
		{"git status", "git status", false, false, ""},
		{
			"compound build and test sets both",
			"go build ./... && go test ./...",
			true, true, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := application.ClassifyCommand(tc.command)
			if got.IsTest != tc.wantTest {
				t.Errorf("IsTest = %v, want %v", got.IsTest, tc.wantTest)
			}
			if got.IsBuild != tc.wantBld {
				t.Errorf("IsBuild = %v, want %v", got.IsBuild, tc.wantBld)
			}
			if got.TestName != tc.wantName {
				t.Errorf("TestName = %q, want %q", got.TestName, tc.wantName)
			}
		})
	}
}

func TestExtractReportedExitCode(t *testing.T) {
	cases := []struct {
		name     string
		response string
		wantCode int
		wantOK   bool
	}{
		{"explicit non-zero", `{"exit_code":1,"stdout":"","stderr":"boom"}`, 1, true},
		{"explicit zero", `{"exit_code":0,"stdout":"ok"}`, 0, true},
		{"no exit field", `{"stdout":"ok","stderr":""}`, 0, false},
		{"not json", `plain text output`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, ok := application.ExtractReportedExitCode([]byte(tc.response))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && code != tc.wantCode {
				t.Errorf("code = %d, want %d", code, tc.wantCode)
			}
		})
	}
}

func TestInferExitCode(t *testing.T) {
	cases := []struct {
		name        string
		response    string
		wantNonZero bool
		wantFailure bool
	}{
		{"contains FAIL", `{"stdout":"--- FAIL: TestX","stderr":""}`, true, true},
		{"clean output", `{"stdout":"ok\nPASS","stderr":""}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, sawFailure := application.InferExitCode([]byte(tc.response))
			if sawFailure != tc.wantFailure {
				t.Fatalf("sawFailure = %v, want %v", sawFailure, tc.wantFailure)
			}
			if tc.wantNonZero && code == 0 {
				t.Errorf("expected non-zero inferred code, got 0")
			}
			if !tc.wantNonZero && code != 0 {
				t.Errorf("expected zero inferred code, got %d", code)
			}
		})
	}
}
