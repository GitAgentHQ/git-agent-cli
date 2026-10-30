package project_test

import (
	"testing"

	"github.com/gitagenthq/git-agent/domain/project"
)

func TestValidateLanguage_ValidCases(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"en", "en"},
		{"EN", "en"},
		{"zh", "zh"},
		{"ja", "ja"},
		{"de", "de"},
		{"fr", "fr"},
		{"es", "es"},
		{"auto", "auto"},
		{"AUTO", "auto"},
		{"Auto", "auto"},
		// Common names normalized to ISO codes
		{"English", "en"},
		{"chinese", "zh"},
		{"Japanese", "ja"},
		{"GERMAN", "de"},
		{"Spanish", "es"},
		{"French", "fr"},
		// Tags with region
		{"en-US", "en-US"},
		{"en-us", "en-US"},
		{"en_us", "en-US"},
		{"zh-CN", "zh-CN"},
		{"zh-cn", "zh-CN"},
		{"zh_CN", "zh-CN"},
		{"zh-TW", "zh-TW"},
		{"pt-BR", "pt-BR"},
		{"pt_br", "pt-BR"},
		// Tags with script and region
		{"zh-Hans-CN", "zh-Hans-CN"},
		{"zh_hans_cn", "zh-Hans-CN"},
		{"zh-Hant-TW", "zh-Hant-TW"},
		// Numeric region code (UN M.49)
		{"es-419", "es-419"},
		// 3-letter ISO 639-2/3
		{"eng", "eng"},
		{"zho", "zho"},
		{"jpn", "jpn"},
	}

	for _, tc := range cases {
		got, err := project.ValidateLanguage(tc.input)
		if err != nil {
			t.Errorf("ValidateLanguage(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ValidateLanguage(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidateLanguage_InvalidCases(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"123",
		"invalid",
		"xx",   // not a valid ISO 639-1 code
		"en-1", // region too short
		"en-TOOLONG",
		"a-b-c-d",
	}

	for _, input := range cases {
		_, err := project.ValidateLanguage(input)
		if err == nil {
			t.Errorf("ValidateLanguage(%q) expected error, got nil", input)
		}
	}
}

func TestIsEnglishLanguage(t *testing.T) {
	trues := []string{"en", "EN", "en-US", "en-us", "en_US", "en-GB", "en-au", "English", "english", "eng", "eng-US"}
	for _, s := range trues {
		if !project.IsEnglishLanguage(s) {
			t.Errorf("IsEnglishLanguage(%q) = false, want true", s)
		}
	}

	falses := []string{"zh", "zh-CN", "ja", "de", "fr", "auto", "", "es"}
	for _, s := range falses {
		if project.IsEnglishLanguage(s) {
			t.Errorf("IsEnglishLanguage(%q) = true, want false", s)
		}
	}
}

func TestResolveLanguageDisplayName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"en", "English"},
		{"zh", "Chinese"},
		{"zh-CN", "Simplified Chinese"},
		{"zh-TW", "Traditional Chinese"},
		{"ja", "Japanese"},
		{"es", "Spanish"},
		{"de", "German"},
		{"fr", "French"},
		{"ko", "Korean"},
		{"pt-BR", "Brazilian Portuguese"},
		{"custom-lang", "custom-lang"},
	}

	for _, tc := range cases {
		got := project.ResolveLanguageDisplayName(tc.input)
		if got != tc.want {
			t.Errorf("ResolveLanguageDisplayName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
