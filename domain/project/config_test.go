package project_test

import (
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
)

func TestDefaultModelCoAuthorDomains_CoversKnownProviders(t *testing.T) {
	// Built-in list must include every common AI provider so enablement of
	// require_model_co_author does not require model_co_author_domains config.
	want := []string{
		"anthropic.com",
		"openai.com",
		"google.com",
		"x.ai",
		"zhipuai.cn",
		"qwen.ai",
		"deepseek.com",
		"moonshot.ai",
	}

	got := map[string]bool{}
	for _, d := range project.DefaultModelCoAuthorDomains {
		got[d] = true
	}

	for _, d := range want {
		if !got[d] {
			t.Errorf("DefaultModelCoAuthorDomains missing %q; got %v", d, project.DefaultModelCoAuthorDomains)
		}
	}
}
