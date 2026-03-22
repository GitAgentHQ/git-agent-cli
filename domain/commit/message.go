package commit

import "strings"

// CommitMessage represents a generated commit message.
type CommitMessage struct {
	Title       string
	Bullets     []string // each entry is the bullet text without "- " prefix
	Explanation string
}

// Body assembles the body section for a git commit message.
// Returns "" when both Bullets and Explanation are empty.
func (m CommitMessage) Body() string {
	if len(m.Bullets) == 0 && m.Explanation == "" {
		return ""
	}
	var sb strings.Builder
	for i, b := range m.Bullets {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString("- ")
		sb.WriteString(b)
	}
	if m.Explanation != "" {
		if len(m.Bullets) > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(m.Explanation)
	}
	return sb.String()
}
