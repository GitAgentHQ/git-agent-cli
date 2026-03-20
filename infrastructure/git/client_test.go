package git

import (
	"testing"
)

func TestGitUnquote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "passthrough unquoted",
			input: "main.go",
			want:  "main.go",
		},
		{
			name:  "strips quotes",
			input: `"main.go"`,
			want:  "main.go",
		},
		{
			name:  "unescape backslash",
			input: `"path\\file"`,
			want:  `path\file`,
		},
		{
			name:  "unescape double quote",
			input: `"say\"hi\""`,
			want:  `say"hi"`,
		},
		{
			name:  "unescape tab",
			input: `"col1\tcol2"`,
			want:  "col1\tcol2",
		},
		{
			name:  "unescape newline",
			input: `"line1\nline2"`,
			want:  "line1\nline2",
		},
		{
			name:  "octal UTF-8 sequence",
			// \303\251 is the UTF-8 encoding of é
			input: `"\303\251"`,
			want:  "é",
		},
		{
			name:  "empty quoted string",
			input: `""`,
			want:  "",
		},
		{
			name:  "not quoted - only opening quote",
			input: `"hello`,
			want:  `"hello`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitUnquote(tt.input)
			if got != tt.want {
				t.Errorf("gitUnquote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseNameStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "modified file",
			input: "M\tmain.go\n",
			want:  []string{"main.go"},
		},
		{
			name:  "added file",
			input: "A\tnew.go\n",
			want:  []string{"new.go"},
		},
		{
			name:  "deleted file",
			input: "D\told.go\n",
			want:  []string{"old.go"},
		},
		{
			name:  "rename emits both paths",
			input: "R100\told.go\tnew.go\n",
			want:  []string{"old.go", "new.go"},
		},
		{
			name:  "copy emits both paths",
			input: "C100\tsrc.go\tdst.go\n",
			want:  []string{"src.go", "dst.go"},
		},
		{
			name:  "mixed lines",
			input: "M\tmain.go\nR100\ta.go\tb.go\nD\tc.go\n",
			want:  []string{"main.go", "a.go", "b.go", "c.go"},
		},
		{
			name:  "quoted path",
			input: "M\t\"path with spaces.go\"\n",
			want:  []string{"path with spaces.go"},
		},
		{
			name:  "deduplication",
			input: "M\tmain.go\nM\tmain.go\n",
			want:  []string{"main.go"},
		},
		{
			name:  "empty input returns nil",
			input: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNameStatus([]byte(tt.input))
			if len(got) != len(tt.want) {
				t.Fatalf("parseNameStatus() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseNameStatus()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
