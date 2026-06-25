package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecide_ExplicitFlags(t *testing.T) {
	cases := []struct {
		name               string
		jsonFlag, textFlag bool
		want               Format
	}{
		{"json wins", true, false, FormatJSON},
		{"text wins", false, true, FormatText},
		{"json takes precedence over text", true, true, FormatJSON},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Decide(c.jsonFlag, c.textFlag); got != c.want {
				t.Fatalf("Decide(%v,%v)=%v, want %v", c.jsonFlag, c.textFlag, got, c.want)
			}
		})
	}
}

func TestEncodeJSON_Indented(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeJSON(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if got["a"] != 1 {
		t.Fatalf("got %v", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("  ")) {
		t.Fatalf("expected indented output, got %q", buf.String())
	}
}
