// Package errors defines git-agent's typed exit codes and error carriers.
//
// Process exit-code taxonomy (mapped in cmd/root.go exitFromError):
//
//	0  success
//	1  general error (any plain error, or NewExitCodeError(1, ...))
//	2  hook blocked the commit after retries (application.ErrHookBlocked)
//	3  retired (formerly "graph not indexed"; co-change reads now auto-index)
//	4  event-log chain integrity check failed
package errors

import "errors"

// ErrNothingToCommit signals that `git commit` found no staged changes. Git
// reports this on stdout (not stderr) with exit code 1; callers match it to skip
// an empty commit group instead of aborting the whole run.
var ErrNothingToCommit = errors.New("nothing to commit")

// ExitCodeError carries an exit code alongside the error message.
type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string {
	return e.Message
}

func NewExitCodeError(code int, msg string) *ExitCodeError {
	return &ExitCodeError{Code: code, Message: msg}
}

// Sentinel exit-code errors. See the package doc for the full taxonomy.
// (Exit code 3 is retired: the AST reads that returned "graph not indexed" are
// gone, and the co-change reads auto-index on first run.)
var (
	// ErrChainIntegrity (4) is returned when the Event Log hash chain fails
	// verification (audit verify / diagnose).
	ErrChainIntegrity = NewExitCodeError(4, "error: event chain integrity check failed")
)

// APIError represents an error returned by the LLM API (rate limit, auth failure, etc.).
type APIError struct {
	HTTPStatusCode int
	Message        string
}

func (e *APIError) Error() string {
	return e.Message
}

func NewAPIError(statusCode int, message string) *APIError {
	return &APIError{HTTPStatusCode: statusCode, Message: message}
}
