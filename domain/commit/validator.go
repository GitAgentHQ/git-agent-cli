package commit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Severity indicates how serious a validation issue is.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

// ValidationIssue is a single finding from commit message validation.
type ValidationIssue struct {
	Severity Severity
	Message  string
}

// ValidationResult holds all findings from ValidateConventional.
type ValidationResult struct {
	Issues []ValidationIssue
}

// HasErrors reports whether any error-severity issues were found.
func (r *ValidationResult) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns error-severity issue messages.
func (r *ValidationResult) Errors() []string {
	var out []string
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			out = append(out, i.Message)
		}
	}
	return out
}

// Warnings returns warning-severity issue messages.
func (r *ValidationResult) Warnings() []string {
	var out []string
	for _, i := range r.Issues {
		if i.Severity == SeverityWarning {
			out = append(out, i.Message)
		}
	}
	return out
}

var (
	headerRe = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|chore|build|ci|revert)(\([a-z0-9_-]+\))?!?: .+`)
	scopeRe  = regexp.MustCompile(`^\w+\(([a-z0-9_-]+)\)`)
	// coAuthorRe matches the strict "Name <email@domain>" form. Git itself
	// treats trailers as free-form text (Rule 9 accepts any non-empty value);
	// this stricter pattern is only needed where an email is semantically
	// required, i.e. model-domain attribution policy.
	coAuthorRe = regexp.MustCompile(`^Co-Authored-By: .+ <[^>]+@[^>]+>$`)
	footerRe   = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*|BREAKING CHANGE): `)
	pastVerbs  = []string{
		"added", "removed", "updated", "changed", "fixed", "created", "deleted",
		"modified", "implemented", "refactored", "renamed", "moved", "replaced",
		"improved", "enhanced", "upgraded", "downgraded", "reverted", "resolved",
	}
)

// ValidateConventional validates a raw commit message using the default
// English rules. It is retained for callers that do not have language context.
func ValidateConventional(raw string, allowedScopes []string) *ValidationResult {
	return ValidateConventionalWithLanguage(raw, allowedScopes, "English", "")
}

// ValidateConventionalWithLanguage validates a raw commit message against
// Conventional Commits and project-specific rules. An explicit English
// language keeps the historical lowercase and byte-count rules. For auto,
// an intent containing a non-ASCII letter selects non-English rules; an empty
// or otherwise undetectable intent remains English.
func ValidateConventionalWithLanguage(raw string, allowedScopes []string, language, intent string) *ValidationResult {
	result := &ValidationResult{}

	if strings.TrimSpace(raw) == "" {
		result.Issues = append(result.Issues, ValidationIssue{SeverityError, "commit message is empty"})
		return result
	}

	lines := strings.Split(raw, "\n")
	nonEnglish := isNonEnglishLanguage(language, intent)
	checkHeader(result, lines[0], allowedScopes, nonEnglish)
	checkBody(result, lines)
	return result
}

func isNonEnglishLanguage(language, intent string) bool {
	language = strings.TrimSpace(language)
	if language == "" || strings.EqualFold(language, "auto") {
		for _, r := range intent {
			if r > unicode.MaxASCII && unicode.IsLetter(r) {
				return true
			}
		}
		return false
	}
	return !isEnglishLanguage(language)
}

func isEnglishLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "english", "en", "en-us", "en-gb", "en-au", "en-ca":
		return true
	default:
		return false
	}
}

func checkHeader(result *ValidationResult, header string, allowedScopes []string, nonEnglish bool) {
	// Rule 1: format
	if !headerRe.MatchString(header) {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			"header must match: <type>[(<scope>)][!]: <description>  " +
				"(valid types: feat fix docs style refactor perf test chore build ci revert)",
		})
		// Rules 3 and 4 can still run on the raw header string.
	}

	// Rule 1b: scope must be in allowed list (when configured)
	if len(allowedScopes) > 0 {
		if m := scopeRe.FindStringSubmatch(header); m != nil {
			scope := m[1]
			found := false
			for _, s := range allowedScopes {
				if s == scope {
					found = true
					break
				}
			}
			if !found {
				result.Issues = append(result.Issues, ValidationIssue{
					SeverityError,
					fmt.Sprintf("scope %q is not in the allowed list: %s", scope, strings.Join(allowedScopes, ", ")),
				})
			}
		}
	}

	// Rule 3: title <=50 characters. UTF-8 bytes are the historical English
	// behavior; natural-language output uses Unicode runes.
	titleLength := len(header)
	if nonEnglish {
		titleLength = utf8.RuneCountInString(header)
	}
	if titleLength > 50 {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			fmt.Sprintf("title must be 50 characters or less (got %d)", titleLength),
		})
	}

	// Rule 4: must not end with '.'
	if strings.HasSuffix(header, ".") {
		result.Issues = append(result.Issues, ValidationIssue{SeverityError, "title must not end with a period"})
	}

	colonIdx := strings.Index(header, ": ")
	if colonIdx == -1 {
		return
	}
	desc := header[colonIdx+2:]

	// Rule 2: English descriptions must be all lowercase. Other languages
	// retain their natural capitalization and orthography.
	if !nonEnglish && desc != strings.ToLower(desc) {
		result.Issues = append(result.Issues, ValidationIssue{SeverityError, "description must be all lowercase"})
	}

	// Warning W1: past-tense verb in description
	fields := strings.Fields(desc)
	if len(fields) > 0 {
		firstWord := strings.ToLower(fields[0])
		for _, v := range pastVerbs {
			if firstWord == v {
				result.Issues = append(result.Issues, ValidationIssue{
					SeverityWarning,
					fmt.Sprintf("description starts with past-tense verb %q — prefer imperative mood", firstWord),
				})
				break
			}
		}
	}
}

func checkBody(result *ValidationResult, lines []string) {
	if len(lines) < 3 {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			"body is required: add bullet points followed by an explanation paragraph",
		})
		return
	}

	if lines[1] != "" {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			"blank line required between header and body",
		})
	}

	bodyLines := lines[2:]
	checkBullets(result, bodyLines)
	checkBodyLineLength(result, bodyLines)
	checkCoAuthoredBy(result, bodyLines)
}

func checkBullets(result *ValidationResult, bodyLines []string) {
	lastBulletIdx := -1
	var bulletFirstWords []string

	for i, line := range bodyLines {
		if strings.HasPrefix(line, "- ") {
			lastBulletIdx = i
			fields := strings.Fields(line[2:])
			if len(fields) > 0 {
				bulletFirstWords = append(bulletFirstWords, strings.ToLower(fields[0]))
			}
		}
	}

	// Rule 6: body must contain bullet points
	if lastBulletIdx == -1 {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			"body must contain at least one bullet point starting with '- '",
		})
		return
	}

	// Rule 8: explanation paragraph required after last bullet
	hasExplanation := false
	for i := lastBulletIdx + 1; i < len(bodyLines); i++ {
		line := bodyLines[i]
		if line != "" && !footerRe.MatchString(line) && !strings.HasPrefix(line, "- ") {
			hasExplanation = true
			break
		}
	}
	if !hasExplanation {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			"explanation paragraph required after bullet points",
		})
	}

	// Warning W2: bullet starts with past-tense verb
	for _, word := range bulletFirstWords {
		for _, v := range pastVerbs {
			if word == v {
				result.Issues = append(result.Issues, ValidationIssue{
					SeverityWarning,
					fmt.Sprintf("bullet starts with past-tense verb %q — prefer imperative mood", word),
				})
				break
			}
		}
	}
}

func checkBodyLineLength(result *ValidationResult, bodyLines []string) {
	// Rule 7: body lines <=72 chars, excluding footers
	for _, line := range bodyLines {
		if footerRe.MatchString(line) {
			continue
		}
		if len(line) > 72 {
			result.Issues = append(result.Issues, ValidationIssue{
				SeverityWarning,
				fmt.Sprintf("body line exceeds 72 characters: %q", line),
			})
		}
	}
}

func checkCoAuthoredBy(result *ValidationResult, bodyLines []string) {
	// Rule 9: git commits any trailer text, so a Co-Authored-By line is valid
	// with a plain name or with the Name <email@domain> form; only an empty
	// value is malformed.
	for _, line := range bodyLines {
		if strings.HasPrefix(line, "Co-Authored-By:") && strings.TrimSpace(line[len("Co-Authored-By:"):]) == "" {
			result.Issues = append(result.Issues, ValidationIssue{
				SeverityError,
				"Co-Authored-By must have a value: Co-Authored-By: Name or Co-Authored-By: Name <email@domain>",
			})
		}
	}
}

// ValidateModelCoAuthor enforces the require_model_co_author policy: at least
// one well-formed Co-Authored-By line in the message must carry an email whose
// domain is in allowedDomains (case-insensitive). Malformed Co-Authored-By
// lines are ignored here — ValidateConventional already reports them.
//
// The allowedDomains slice is taken as-is; callers typically pass
// project.DefaultModelCoAuthorDomains directly.
func ValidateModelCoAuthor(raw string, allowedDomains []string) *ValidationResult {
	result := &ValidationResult{}
	normalized := normalizeDomains(allowedDomains)

	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "Co-Authored-By:") || !coAuthorRe.MatchString(line) {
			continue
		}
		domain := extractEmailDomain(line)
		if domain != "" && containsDomain(normalized, domain) {
			return result
		}
	}

	result.Issues = append(result.Issues, ValidationIssue{
		SeverityError,
		fmt.Sprintf("commit must include a Co-Authored-By trailer from one of: %s", strings.Join(normalized, ", ")),
	})
	return result
}

// HasModelCoAuthor reports whether trailers contains at least one
// Co-Authored-By entry whose email domain is in allowedDomains
// (case-insensitive). Use this for fail-fast pre-flight at the cmd layer,
// before the message body is assembled or the LLM is called.
func HasModelCoAuthor(trailers []Trailer, allowedDomains []string) bool {
	normalized := normalizeDomains(allowedDomains)
	if len(normalized) == 0 {
		return false
	}
	for _, t := range trailers {
		if !strings.EqualFold(t.Key, "Co-Authored-By") {
			continue
		}
		domain := extractEmailDomain(t.Value)
		if domain != "" && containsDomain(normalized, domain) {
			return true
		}
	}
	return false
}

func normalizeDomains(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

func containsDomain(normalized []string, want string) bool {
	for _, d := range normalized {
		if d == want {
			return true
		}
	}
	return false
}

// ModelProviderInfo defines a recognized AI provider's model naming pattern & domain metadata.
type ModelProviderInfo struct {
	MatchKeyword string // Substring to identify provider (e.g. "gemini", "claude", "deepseek")
	Domain       string // Canonical email domain (e.g. "google.com")
}

// fallbackCoAuthorDomain is the git-agent-owned email domain used when a
// session model maps to no known provider (stealth aliases, custom gateways).
// It is part of the default model co-author allow-list
// (project.DefaultModelCoAuthorDomains).
const fallbackCoAuthorDomain = "models.git-agent.dev"

// knownProviders is the table-driven registry of recognized AI model providers used for co-author inference.
var knownProviders = []ModelProviderInfo{
	{MatchKeyword: "gemini", Domain: "google.com"},
	{MatchKeyword: "claude", Domain: "anthropic.com"},
	{MatchKeyword: "gpt", Domain: "openai.com"},
	{MatchKeyword: "codex", Domain: "openai.com"},
	{MatchKeyword: "o1", Domain: "openai.com"},
	{MatchKeyword: "o3", Domain: "openai.com"},
	{MatchKeyword: "deepseek", Domain: "deepseek.com"},
	{MatchKeyword: "qwen", Domain: "qwen.ai"},
	{MatchKeyword: "glm", Domain: "zhipuai.cn"},
	{MatchKeyword: "kimi", Domain: "moonshot.ai"},
	{MatchKeyword: "moonshot", Domain: "moonshot.ai"},
	{MatchKeyword: "grok", Domain: "x.ai"},
}

// ignoredTierSuffixes is an ordered table of routing, reasoning, and tier suffixes to strip.
// Longer composite suffixes (e.g. "non-reasoning") precede shorter sub-tokens ("reasoning").
var ignoredTierSuffixes = []string{
	"non-reasoning",
	"reasoning",
	"thinking",
	"high",
	"medium",
	"low",
	"minimal",
	"xhigh",
	"free",
}

// knownBrandCasings defines canonical display casings for common model terminology.
var knownBrandCasings = map[string]string{
	"gpt":      "GPT",
	"glm":      "GLM",
	"ai":       "AI",
	"deepseek": "DeepSeek",
	"qwen":     "Qwen",
	"kimi":     "Kimi",
	"grok":     "Grok",
	"claude":   "Claude",
	"gemini":   "Gemini",
}

// InferModelCoAuthor infers a Co-Authored-By trailer from a model ID string using a table-driven design.
// Known providers contribute their canonical email domain; models that map to
// no known provider (stealth aliases, custom gateways) are attributed under
// fallbackCoAuthorDomain. Returns (Trailer{}, false) only for an empty model ID.
func InferModelCoAuthor(modelID string) (Trailer, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return Trailer{}, false
	}

	// 1. Strip provider routing prefixes (e.g. "opencode/", "bailian/", "ark/", "loli/")
	cleaned := modelID
	if idx := strings.LastIndex(cleaned, "/"); idx != -1 {
		cleaned = cleaned[idx+1:]
	}

	lowerClean := strings.ToLower(cleaned)

	// 2. Table lookup for known provider matching; unmatched models keep the
	// fallback domain so attribution never requires provider mapping.
	domain := fallbackCoAuthorDomain
	for i := range knownProviders {
		p := &knownProviders[i]
		if strings.Contains(lowerClean, p.MatchKeyword) {
			domain = p.Domain
			break
		}
	}

	// 3. Strip trailing reasoning/tier suffixes and date tags (-YYYYMMDD or -MMDD)
	for {
		changed := false
		lower := strings.ToLower(cleaned)
		for _, suffix := range ignoredTierSuffixes {
			hyphenSuffix := "-" + suffix
			if strings.HasSuffix(lower, hyphenSuffix) {
				cleaned = cleaned[:len(cleaned)-len(hyphenSuffix)]
				changed = true
				break
			}
		}
		if changed {
			continue
		}
		if idx := strings.LastIndex(cleaned, "-"); idx != -1 {
			datePart := cleaned[idx+1:]
			if (len(datePart) == 8 || len(datePart) == 4) && isDigits(datePart) {
				cleaned = cleaned[:idx]
				continue
			}
		}
		break
	}

	// 4. Tokenize and format canonical display title using brand casing table
	parts := strings.FieldsFunc(cleaned, func(r rune) bool {
		return r == '-' || r == '_'
	})

	for i, p := range parts {
		pLower := strings.ToLower(p)
		if c, ok := knownBrandCasings[pLower]; ok {
			parts[i] = c
		} else if strings.HasPrefix(pLower, "qwen") && len(pLower) > 4 {
			parts[i] = "Qwen " + pLower[4:]
		} else if len(pLower) >= 2 && pLower[0] == 'v' && isDigits(pLower[1:]) {
			parts[i] = "V" + pLower[1:]
		} else {
			parts[i] = capitalize(p)
		}
	}

	// 5. Join adjacent numbers with '.' if model ID used '-' between version digits (e.g. claude-3-5 -> Claude 3.5)
	var mergedParts []string
	for i := 0; i < len(parts); i++ {
		if i > 0 && isDigits(parts[i-1]) && isDigits(parts[i]) {
			mergedParts[len(mergedParts)-1] = mergedParts[len(mergedParts)-1] + "." + parts[i]
		} else {
			mergedParts = append(mergedParts, parts[i])
		}
	}

	title := strings.Join(mergedParts, " ")
	return Trailer{
		Key:   "Co-Authored-By",
		Value: fmt.Sprintf("%s <noreply@%s>", title, domain),
	}, true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isTierSuffix(s string) bool {
	switch s {
	case "high", "medium", "low", "minimal", "xhigh", "max", "lite", "extra", "thinking", "reasoning", "non-reasoning":
		return true
	default:
		return false
	}
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// extractEmailDomain returns the lowercased domain from the last <...@...>
// pair on the line. Callers should pre-validate format with coAuthorRe.
func extractEmailDomain(line string) string {
	openIdx := strings.LastIndex(line, "<")
	closeIdx := strings.LastIndex(line, ">")
	if openIdx == -1 || closeIdx == -1 || closeIdx <= openIdx {
		return ""
	}
	email := line[openIdx+1 : closeIdx]
	at := strings.LastIndex(email, "@")
	if at == -1 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
