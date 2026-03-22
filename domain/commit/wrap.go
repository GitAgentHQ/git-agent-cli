package commit

import "strings"

// WrapBody wraps body lines to at most width characters.
// Bullet lines (starting with "- ") use a 2-space continuation indent.
// Paragraph lines use no continuation indent.
// Blank lines are preserved unchanged.
func WrapBody(body string, width int) string {
	if body == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) []string {
	if line == "" || len(line) <= width {
		return []string{line}
	}

	var indent string
	if strings.HasPrefix(line, "- ") {
		indent = "  "
	}

	var result []string
	for len(line) > width {
		cut := lastWordBreak(line, width)
		if cut < 0 {
			result = append(result, line[:width])
			line = indent + line[width:]
		} else {
			result = append(result, line[:cut])
			line = indent + line[cut+1:]
		}
	}
	if line != "" {
		result = append(result, line)
	}
	return result
}

// lastWordBreak returns the index of the last space at or before width,
// starting the search from index 2 to skip bullet/indent prefixes.
// Returns -1 if no suitable break point is found.
func lastWordBreak(s string, width int) int {
	end := width
	if end >= len(s) {
		end = len(s) - 1
	}
	for i := end; i >= 2; i-- {
		if s[i] == ' ' {
			return i
		}
	}
	return -1
}
