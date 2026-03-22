package commit

import "strings"

// WrapExplanation wraps explanation paragraph lines that exceed 100 characters
// to a target of ~72 characters at a word boundary. Lines at or under 100
// characters are passed through unchanged.
func WrapExplanation(text string, width int) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) <= 100 {
			out = append(out, line)
		} else {
			out = append(out, wrapLong(line, width)...)
		}
	}
	return strings.Join(out, "\n")
}

// wrapLong breaks a single long line into segments of at most width characters,
// splitting at word boundaries.
func wrapLong(line string, width int) []string {
	var result []string
	for len(line) > width {
		cut := lastSpace(line, width)
		if cut < 0 {
			result = append(result, line[:width])
			line = line[width:]
		} else {
			result = append(result, line[:cut])
			line = line[cut+1:]
		}
	}
	if line != "" {
		result = append(result, line)
	}
	return result
}

// lastSpace returns the index of the last space at or before width.
// Returns -1 if no space is found.
func lastSpace(s string, width int) int {
	end := width
	if end >= len(s) {
		end = len(s) - 1
	}
	for i := end; i >= 0; i-- {
		if s[i] == ' ' {
			return i
		}
	}
	return -1
}
