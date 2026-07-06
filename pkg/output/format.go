// Package output centralizes query-command output format selection.
//
// Every read command exposes a single -o/--output {auto,json,text} flag. With
// "auto" (the default) the command picks JSON when stdout is piped (for piping
// into jq / other tools) and text when stdout is a TTY (for human reading).
// This package exists so that decision and the JSON encoding live in one place
// instead of being hand-rolled per command.
package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// Format is the selected output representation for a read command.
type Format int

const (
	// FormatJSON emits indented JSON.
	FormatJSON Format = iota
	// FormatText emits the command's human-readable text rendering.
	FormatText
)

// Decide maps the --output flag value to a Format. "json" and "text" select that
// representation explicitly; "auto" (the default, and the fallback for any other
// value) auto-detects: JSON when stdout is not a terminal (piped/redirected),
// otherwise text.
func Decide(output string) Format {
	switch output {
	case "json":
		return FormatJSON
	case "text":
		return FormatText
	default: // "auto" and empty
		if !isatty.IsTerminal(os.Stdout.Fd()) {
			return FormatJSON
		}
		return FormatText
	}
}

// EncodeJSON writes v as indented JSON to w.
func EncodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// EncodeError writes the uniform machine error envelope
// {"error":{"code":<code>,"message":<message>}} as indented JSON to w. Read
// commands call this on failure when the resolved format is JSON, so agents get
// a structured error with the process exit code instead of free-form text.
func EncodeError(w io.Writer, code int, message string) error {
	return EncodeJSON(w, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}
