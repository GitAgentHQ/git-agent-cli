package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSkillsGet_CorePrintsMainGuide(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "get"}
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)
	if err := runSkillsGet(cmd, []string{"core"}); err != nil {
		t.Fatalf("skills get core: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "# Git Agent CLI") {
		t.Errorf("expected core guide heading in output, got: %q", firstLine(got))
	}
	if !strings.Contains(got, "git-agent skills get cli") {
		t.Errorf("expected core to point at `git-agent skills get cli` for the full reference")
	}
}

func TestSkillsGet_CoreDocumentsScopeAndGitignoreOptimization(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "get"}
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)
	if err := runSkillsGet(cmd, []string{"core"}); err != nil {
		t.Fatalf("skills get core: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"Optimize scopes and .gitignore",
		"init --scope --force",
		"init --gitignore",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected core guide to mention %q for scope/.gitignore optimization", want)
		}
	}
}

func TestSkillsGet_CliPrintsCommandReference(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "get"}
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)
	if err := runSkillsGet(cmd, []string{"cli"}); err != nil {
		t.Fatalf("skills get cli: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "git-agent commit") {
		t.Errorf("expected commit entry in command reference, got: %q", firstLine(got))
	}
}

func TestSkillsGet_UnknownDocumentFailsWithAvailableList(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := runSkillsGet(cmd, []string{"nope"})
	if err == nil {
		t.Fatal("expected error for unknown document")
	}
	for _, want := range []string{"core", "cli"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to list %q, got: %v", want, err)
		}
	}
}

func TestSkillsGet_MissingNameFails(t *testing.T) {
	if err := skillsGetCmd.Args(nil, nil); err == nil {
		t.Fatal("expected error when no document name is given")
	} else if !strings.Contains(err.Error(), "document name") {
		t.Errorf("expected hint about a document name, got: %v", err)
	}
}

func TestSkillsList_ShowsEveryDocument(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "list"}
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)
	if err := runSkillsList(cmd, nil); err != nil {
		t.Fatalf("skills list: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"core", "cli"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in list output, got: %q", want, got)
		}
	}
}

func TestSkillsCmd_NoSubcommandPrintsHelp(t *testing.T) {
	prevOut, prevErr := rootCmd.OutOrStderr(), rootCmd.ErrOrStderr()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(prevOut)
		rootCmd.SetErr(prevErr)
	})
	if err := ExecuteArgs([]string{"skills"}); err != nil {
		t.Fatalf("git-agent skills: %v", err)
	}
	if !strings.Contains(buf.String(), "Print git-agent's own usage documentation") {
		t.Errorf("expected skills help in output, got: %q", buf.String())
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
