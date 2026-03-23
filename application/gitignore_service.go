package application

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
)

const (
	autoGenStart  = "### git-agent auto-generated — DO NOT EDIT this block ###"
	autoGenEnd    = "### end git-agent ###"
	customSection = "### custom rules ###"
)

// GitignoreService generates or updates a .gitignore file using AI-detected technologies.
type GitignoreService struct {
	detector  domainGitignore.TechDetector
	generator domainGitignore.ContentGenerator
	git       GitReader
}

func NewGitignoreService(
	detector domainGitignore.TechDetector,
	generator domainGitignore.ContentGenerator,
	git GitReader,
) *GitignoreService {
	return &GitignoreService{
		detector:  detector,
		generator: generator,
		git:       git,
	}
}

// GitignoreRequest holds options for the Generate call.
type GitignoreRequest struct{}

// Generate detects technologies, fetches .gitignore content, and writes .gitignore.
func (s *GitignoreService) Generate(ctx context.Context, req GitignoreRequest) ([]string, error) {
	dirs, err := s.git.TopLevelDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading dirs: %w", err)
	}

	files, err := s.git.ProjectFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading project files: %w", err)
	}

	osName := runtimeOS()

	techs, err := s.detector.DetectTechnologies(ctx, domainGitignore.DetectRequest{
		OS:    osName,
		Dirs:  dirs,
		Files: files,
	})
	if err != nil {
		return nil, fmt.Errorf("detecting technologies: %w", err)
	}

	generated, err := s.generator.Generate(ctx, techs)
	if err != nil {
		return nil, fmt.Errorf("generating gitignore content: %w", err)
	}

	// Use the technology list actually reflected in the Toptal response URL,
	// not the raw LLM output, so the header stays consistent with the content.
	if actual := toptalTechs(generated); len(actual) > 0 {
		techs = actual
	}

	content := wrapGenerated(generated, techs)

	existing, _ := os.ReadFile(".gitignore")
	var final string
	if len(existing) == 0 {
		final = content
	} else {
		final = mergeGitignore(string(existing), content)
	}

	if err := os.WriteFile(".gitignore", []byte(final), 0644); err != nil {
		return nil, fmt.Errorf("writing .gitignore: %w", err)
	}

	return techs, nil
}

// toptalTechs extracts the technology list from the "# Created by" line in a
// Toptal gitignore response, e.g.
//
//	# Created by https://www.toptal.com/developers/gitignore/api/macos,go
//
// returns ["macos", "go"]. Returns nil if the line is absent or malformed.
func toptalTechs(content string) []string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# Created by") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			break
		}
		url := fields[len(fields)-1]
		if idx := strings.LastIndex(url, "/api/"); idx != -1 {
			return strings.Split(url[idx+5:], ",")
		}
		break
	}
	return nil
}

// runtimeOS maps GOOS to Toptal API names.
func runtimeOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

// wrapGenerated wraps the Toptal content in auto-generated markers.
func wrapGenerated(content string, techs []string) string {
	header := fmt.Sprintf("%s\n# Technologies: %s\n", autoGenStart, strings.Join(techs, ", "))
	return header + strings.TrimRight(content, "\n") + "\n" + autoGenEnd + "\n"
}

// mergeGitignore places the auto-generated block first, then appends any user
// rules (from outside the previous auto-gen markers) that are not already
// covered by the generated content, under a "### custom rules ###" header.
func mergeGitignore(existing, generated string) string {
	// Collect user lines — everything outside auto-gen markers.
	// Skip the customSection header itself so it isn't duplicated.
	var userLines []string
	inBlock := false
	for _, line := range strings.Split(existing, "\n") {
		switch strings.TrimSpace(line) {
		case autoGenStart:
			inBlock = true
		case autoGenEnd:
			inBlock = false
		case customSection:
			// skip — we re-emit this header ourselves
		default:
			if !inBlock {
				userLines = append(userLines, line)
			}
		}
	}

	// Build set of patterns already covered by the generated block.
	covered := make(map[string]bool)
	for _, line := range strings.Split(generated, "\n") {
		if p := strings.TrimSpace(line); p != "" && !strings.HasPrefix(p, "#") {
			covered[p] = true
		}
	}

	// Keep only lines that are blank/comments or not already covered.
	var unique []string
	for _, line := range userLines {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") || !covered[p] {
			unique = append(unique, line)
		}
	}
	unique = trimLeadingEmpty(trimTrailingEmpty(unique))

	// Structure: auto-generated block → custom rules section (if any).
	result := strings.TrimRight(generated, "\n") + "\n"
	if len(unique) > 0 {
		result += "\n" + customSection + "\n" + strings.Join(unique, "\n") + "\n"
	}
	return result
}

func trimTrailingEmpty(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func trimLeadingEmpty(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return lines
}
