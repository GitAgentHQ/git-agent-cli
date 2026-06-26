package application

import (
	"encoding/json"
	"strings"
)

// OutcomeClassification is the deterministic verdict for a Bash command.
type OutcomeClassification struct {
	IsTest   bool
	IsBuild  bool
	TestName string
}

// ClassifyCommand deterministically classifies a Bash command as test and/or
// build, extracting a test name from syntax that exposes one (e.g. `go test -run
// <name>`). Recognized test forms include go test, make test, pnpm test, pytest,
// cargo test; build forms include go build, make build. Compound commands joined
// by &&/||/; classify by every segment, so `go build && go test` sets both.
// Unrecognized commands classify as neither.
func ClassifyCommand(command string) OutcomeClassification {
	var c OutcomeClassification
	for _, seg := range splitCommandSegments(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		if classifySegmentTest(fields) {
			c.IsTest = true
			if name := extractGoRunName(fields); name != "" && c.TestName == "" {
				c.TestName = name
			}
		}
		if classifySegmentBuild(fields) {
			c.IsBuild = true
		}
	}
	return c
}

// splitCommandSegments breaks a command line on the &&, ||, and ; separators so
// each invocation in a compound command is classified independently.
func splitCommandSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	return strings.Split(replacer.Replace(command), "\n")
}

// classifySegmentTest reports whether a single command segment is a test run.
func classifySegmentTest(fields []string) bool {
	switch fields[0] {
	case "pytest":
		return true
	case "go", "cargo":
		return hasWord(fields, "test")
	case "make", "pnpm", "npm", "yarn":
		return hasWord(fields, "test")
	}
	return false
}

// classifySegmentBuild reports whether a single command segment is a build.
func classifySegmentBuild(fields []string) bool {
	switch fields[0] {
	case "go", "cargo":
		return hasWord(fields, "build")
	case "make", "pnpm", "npm", "yarn":
		return hasWord(fields, "build")
	}
	return false
}

func hasWord(fields []string, word string) bool {
	for _, f := range fields[1:] {
		if f == word {
			return true
		}
	}
	return false
}

// extractGoRunName returns the value of a `-run <name>` (or `-run=<name>`) flag,
// which `go test` uses to select tests. Empty when no such flag is present.
func extractGoRunName(fields []string) string {
	for i, f := range fields {
		if f == "-run" && i+1 < len(fields) {
			return fields[i+1]
		}
		if strings.HasPrefix(f, "-run=") {
			return strings.TrimPrefix(f, "-run=")
		}
	}
	return ""
}

// ExtractReportedExitCode returns the exit code stated in a Bash tool_response
// and whether one was present. Used to distinguish "reported" from "inferred".
func ExtractReportedExitCode(toolResponse []byte) (code int, ok bool) {
	var resp struct {
		ExitCode *int `json:"exit_code"`
	}
	if err := json.Unmarshal(toolResponse, &resp); err != nil {
		return 0, false
	}
	if resp.ExitCode == nil {
		return 0, false
	}
	return *resp.ExitCode, true
}

// InferExitCode derives a best-effort exit code from output failure markers
// (e.g. "FAIL") when no exit code is reported. The result is always flagged
// "inferred" by the caller and down-weighted in diagnose (best-practices.md
// pitfall 13). Returns the inferred code and whether a failure marker was seen.
func InferExitCode(toolResponse []byte) (code int, sawFailure bool) {
	if containsFailureMarker(toolResponse) {
		return 1, true
	}
	return 0, false
}

// failureMarkers are case-sensitive substrings that signal a failed run when no
// exit code is reported. Kept conservative to avoid mis-tagging passing output.
var failureMarkers = []string{"FAIL", "Error:", "error:", "panic:", "Traceback"}

func containsFailureMarker(toolResponse []byte) bool {
	s := string(toolResponse)
	for _, m := range failureMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
