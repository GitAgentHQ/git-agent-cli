package commit

import (
	"fmt"
	"regexp"
	"strings"
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
	headerRe   = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|chore|build|ci|revert)(\([a-z0-9_-]+\))?!?: .+`)
	scopeRe    = regexp.MustCompile(`^\w+\(([a-z0-9_-]+)\)`)
	coAuthorRe = regexp.MustCompile(`^Co-Authored-By: .+ <[^>]+@[^>]+>$`)
	footerRe   = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*|BREAKING CHANGE): `)
	pastVerbs  = []string{
		"added", "removed", "updated", "changed", "fixed", "created", "deleted",
		"modified", "implemented", "refactored", "renamed", "moved", "replaced",
		"improved", "enhanced", "upgraded", "downgraded", "reverted", "resolved",
	}
)

// ValidateConventional validates a raw commit message against Conventional
// Commits 1.0.0 and project-specific rules. When allowedScopes is non-empty,
// any scope used in the header must be in the list. It never returns nil.
func ValidateConventional(raw string, allowedScopes []string) *ValidationResult {
	result := &ValidationResult{}

	if strings.TrimSpace(raw) == "" {
		result.Issues = append(result.Issues, ValidationIssue{SeverityError, "commit message is empty"})
		return result
	}

	lines := strings.Split(raw, "\n")
	checkHeader(result, lines[0], allowedScopes)
	checkBody(result, lines)
	return result
}

func checkHeader(result *ValidationResult, header string, allowedScopes []string) {
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

	// Rule 3: title <=50 chars
	if len(header) > 50 {
		result.Issues = append(result.Issues, ValidationIssue{
			SeverityError,
			fmt.Sprintf("title must be 50 characters or less (got %d)", len(header)),
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

	// Rule 2: description must be all lowercase
	if desc != strings.ToLower(desc) {
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
	// Rule 9: Co-Authored-By format, when present
	for _, line := range bodyLines {
		if strings.HasPrefix(line, "Co-Authored-By:") {
			if !coAuthorRe.MatchString(line) {
				result.Issues = append(result.Issues, ValidationIssue{
					SeverityError,
					"Co-Authored-By must be: Co-Authored-By: Name <email@domain>",
				})
			}
		}
	}
}

// ValidateModelCoAuthor enforces the require_model_co_author policy: at least
// one well-formed Co-Authored-By line in the message must carry an email whose
// domain is in allowedDomains (case-insensitive). Malformed Co-Authored-By
// lines are ignored here — ValidateConventional already reports them.
//
// The allowedDomains slice is taken as-is; callers are expected to merge
// project.DefaultModelCoAuthorDomains with any user-configured extensions
// before calling.
func ValidateModelCoAuthor(raw string, allowedDomains []string) *ValidationResult {
	result := &ValidationResult{}

	normalized := make([]string, 0, len(allowedDomains))
	for _, d := range allowedDomains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" {
			normalized = append(normalized, d)
		}
	}

	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "Co-Authored-By:") || !coAuthorRe.MatchString(line) {
			continue
		}
		domain := extractEmailDomain(line)
		if domain == "" {
			continue
		}
		for _, d := range normalized {
			if domain == d {
				return result
			}
		}
	}

	result.Issues = append(result.Issues, ValidationIssue{
		SeverityError,
		fmt.Sprintf("commit must include a Co-Authored-By trailer from one of: %s", strings.Join(normalized, ", ")),
	})
	return result
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
