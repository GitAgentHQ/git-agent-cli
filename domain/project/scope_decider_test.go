package project_test

import (
	"reflect"
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
)

func TestCandidateDirs_FiltersNoiseAndKeepsOrder(t *testing.T) {
	dirs := []string{
		"application", "node_modules", ".github", "cmd", "vendor",
		"Docs", "dist", "domain", "target", "infrastructure",
	}
	want := []string{"application", "cmd", "domain", "infrastructure"}

	if got := project.CandidateDirs(dirs); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestCandidateDirs_DropsDuplicates(t *testing.T) {
	got := project.CandidateDirs([]string{"cmd", "cmd", "CMD", " cmd "})
	want := []string{"cmd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestDeriveScopeName(t *testing.T) {
	cases := []struct {
		dir  string
		want string
	}{
		{"application", "app"},
		{"infrastructure", "infra"},
		{"cmd", "cli"},
		{"domain", "domain"},
		{"my-web-app", "mywebapp"},
		{"git-agent", "gitagent"},
		{"scope/nested", "nested"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := project.DeriveScopeName(tc.dir); got != tc.want {
			t.Errorf("DeriveScopeName(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

func TestScopeDecision_CreatedSkipsReuseAndSkip(t *testing.T) {
	decision := &project.ScopeDecision{Assignments: []project.ScopeAssignment{
		{Dir: "cmd", Action: project.ScopeReuse, Scope: "cli"},
		{Dir: "application", Action: project.ScopeCreate, Scope: "app"},
		{Dir: "scripts", Action: project.ScopeSkip},
		{Dir: "empty", Action: project.ScopeCreate},
	}}

	got := decision.Created()
	want := []project.ProposedScope{{Name: "app", Dir: "application"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
