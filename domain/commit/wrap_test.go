package commit_test

import (
	"strings"
	"testing"

	"github.com/gitagenthq/git-agent/domain/commit"
)

func TestWrapBody(t *testing.T) {
	const w = 72

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty string unchanged",
			in:   "",
			want: "",
		},
		{
			name: "short line unchanged",
			in:   "- short bullet\n\nShort paragraph.",
			want: "- short bullet\n\nShort paragraph.",
		},
		{
			name: "line exactly 72 chars unchanged",
			in:   "- " + strings.Repeat("x", 70),
			want: "- " + strings.Repeat("x", 70),
		},
		{
			name: "blank lines preserved",
			in:   "- bullet one\n\n- bullet two\n\nParagraph.",
			want: "- bullet one\n\n- bullet two\n\nParagraph.",
		},
		{
			// 75 chars: last space within width=72 is at index 71 (before "now")
			name: "bullet line over 72 wraps with 2-space indent",
			in:   "- add route handler for the new login endpoint that is being introduced now",
			want: "- add route handler for the new login endpoint that is being introduced\n  now",
		},
		{
			// 83 chars: last space within 72 is after "one" at index 72
			name: "paragraph line over 72 wraps without indent",
			in:   "This introduces the new authentication system which replaces the old one completely.",
			want: "This introduces the new authentication system which replaces the old one\ncompletely.",
		},
		{
			// 82 chars: no space from index 2..72, hard-breaks at 72
			name: "single long word hard-breaks at width",
			in:   "- " + strings.Repeat("a", 80),
			want: "- " + strings.Repeat("a", 70) + "\n  " + strings.Repeat("a", 10),
		},
		{
			name: "multi-line body wraps only long lines",
			in:   "- short bullet\n- add route handler for the new login endpoint that is being introduced now\n\nThis paragraph is short.",
			want: "- short bullet\n- add route handler for the new login endpoint that is being introduced\n  now\n\nThis paragraph is short.",
		},
		{
			name: "full body with bullets and paragraph",
			in:   "- add route handler for the new login endpoint that is being introduced now\n- update tests\n\nThis introduces basic authentication support for the application.",
			want: "- add route handler for the new login endpoint that is being introduced\n  now\n- update tests\n\nThis introduces basic authentication support for the application.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := commit.WrapBody(tc.in, w)
			if got != tc.want {
				t.Errorf("WrapBody(%q, %d)\ngot:  %q\nwant: %q", tc.in, w, got, tc.want)
			}
		})
	}
}
