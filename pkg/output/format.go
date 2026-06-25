// Package output centralizes query-command output format selection.
//
// Every graph query command exposes --json and --text flags. When neither is
// set, the command auto-detects: JSON when stdout is piped (for piping into
// jq / other tools), text when stdout is a TTY (for human reading). This
// package exists so that decision and the JSON encoding live in one place
// instead of being hand-rolled per command.
package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// Format is the selected output representation for a query command.
type Format int

const (
	// FormatJSON emits indented JSON.
	FormatJSON Format = iota
	// FormatText emits the command's human-readable text rendering.
	FormatText
)

// Decide picks the output format from the --json/--text flag pair. An explicit
// flag always wins; when neither is set, JSON is chosen when stdout is not a
// terminal (i.e. piped/redirected), otherwise text.
func Decide(jsonFlag, textFlag bool) Format {
	if jsonFlag {
		return FormatJSON
	}
	if textFlag {
		return FormatText
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return FormatJSON
	}
	return FormatText
}

// EncodeJSON writes v as indented JSON to w.
func EncodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
