package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecide_Explicit(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"json", FormatJSON},
		{"text", FormatText},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := Decide(c.in); got != c.want {
				t.Fatalf("Decide(%q)=%v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDecide_AutoPipedIsJSON(t *testing.T) {
	// Under `go test`, stdout is not a TTY, so auto resolves to JSON.
	for _, in := range []string{"auto", ""} {
		if got := Decide(in); got != FormatJSON {
			t.Fatalf("Decide(%q)=%v, want FormatJSON when piped", in, got)
		}
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

func TestEncodeError_Envelope(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeError(&buf, 3, "graph not indexed"); err != nil {
		t.Fatalf("EncodeError: %v", err)
	}
	var got struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if got.Error.Code != 3 || got.Error.Message != "graph not indexed" {
		t.Fatalf("got %+v", got.Error)
	}
}
