package gitignore

import "context"

// DetectRequest holds project information for technology detection.
type DetectRequest struct {
	OS    string
	Dirs  []string
	Files []string
}

// TechDetector analyzes a project and returns Toptal-compatible technology identifiers.
type TechDetector interface {
	DetectTechnologies(ctx context.Context, req DetectRequest) ([]string, error)
}
