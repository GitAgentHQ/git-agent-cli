package e2e_test

import (
	"strings"
	"testing"
)

func TestSkillsGetCore_PrintsMainGuide(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills", "get", "core")
	if code != 0 {
		t.Fatalf("git-agent skills get core: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "# Git Agent CLI") {
		t.Errorf("expected core guide in output, got: %s", out)
	}
	if !strings.Contains(out, "git-agent skills get cli") {
		t.Errorf("expected core to point at `git-agent skills get cli`, got: %s", out)
	}
}

func TestSkillsGetCli_PrintsCommandReference(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills", "get", "cli")
	if code != 0 {
		t.Fatalf("git-agent skills get cli: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "git-agent commit") {
		t.Errorf("expected command reference in output, got: %s", out)
	}
}

func TestSkillsGet_UnknownDocumentFailsWithAvailableList(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills", "get", "nope")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "core") || !strings.Contains(out, "cli") {
		t.Errorf("expected available documents in error, got: %s", out)
	}
}

func TestSkillsGet_NoNameFails(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills", "get")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "document name") {
		t.Errorf("expected hint about a document name, got: %s", out)
	}
}

func TestSkillsList_ShowsEveryDocument(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills", "list")
	if code != 0 {
		t.Fatalf("git-agent skills list: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "core") || !strings.Contains(out, "cli") {
		t.Errorf("expected core and cli in list, got: %s", out)
	}
}

func TestSkills_NoSubcommandPrintsHelp(t *testing.T) {
	out, code := gitAgent(t, t.TempDir(), "skills")
	if code != 0 {
		t.Fatalf("git-agent skills: exit code %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Print git-agent's own usage documentation") {
		t.Errorf("expected skills help in output, got: %s", out)
	}
}
