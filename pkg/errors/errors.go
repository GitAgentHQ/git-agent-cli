package errors

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

// Sentinel errors
var (
	ErrNoStagedChanges = NewExitCodeError(1, "error: no staged changes to commit")
	ErrNotGitRepo      = NewExitCodeError(1, "error: not a git repository")
	ErrGitNotFound     = NewExitCodeError(1, "error: git not found in PATH")
	ErrGraphNotIndexed = NewExitCodeError(3, "error: graph not indexed")
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
