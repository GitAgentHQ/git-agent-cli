package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// Resolve the .gitignore path at the repo root, not the process cwd, so
	// init invoked from a subdirectory writes the rule into the root .gitignore
	// (mirroring the cwd-independence of the git client methods).
	root, err := s.git.RepoRoot(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo root: %w", err)
	}
	gitignorePath := filepath.Join(root, ".gitignore")

	existing, _ := os.ReadFile(gitignorePath)
	var final string
	if len(existing) == 0 {
		final = content
	} else {
		final = mergeGitignore(string(existing), content)
	}

	// Generated files (graph DB, local config) must never be tracked. Inject
	// their ignore rules mandatorily and idempotently so they survive every
	// regeneration even when the Toptal content omits them.
	final = EnsureMandatoryIgnoreRules(final)

	if err := os.WriteFile(gitignorePath, []byte(final), 0644); err != nil {
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

// mandatoryIgnoreRules are ignore entries git-agent injects into every
// .gitignore because they describe generated files that must never be tracked.
//   - .git-agent/graph.db + sidecars: the runtime SQLite graph database. If
//     tracked, auto-staging re-adds it every run (the "infinite recreation"
//     loop of chore commits).
//   - .git-agent/config.local.yml: personal per-repo overrides (see config
//     --local); documented as gitignored, and the ignore rule must hold.
var mandatoryIgnoreRules = []string{
	".git-agent/graph.db",
	"*.db-shm",
	"*.db-wal",
	"*.db-journal",
	".git-agent/config.local.yml",
}

// EnsureMandatoryIgnoreRules guarantees the mandatory ignore rules are present
// exactly once. If any rule is missing it appends a dedicated, idempotent block
// after the existing content; rules already present (anywhere in the file) are
// left untouched so the block is not duplicated across regenerations.
func EnsureMandatoryIgnoreRules(content string) string {
	var missing []string
	for _, rule := range mandatoryIgnoreRules {
		if !gitignoreHasRule(content, rule) {
			missing = append(missing, rule)
		}
	}
	if len(missing) == 0 {
		return content
	}
	block := "\n# git-agent generated files (never track)\n" +
		strings.Join(missing, "\n") + "\n"
	return strings.TrimRight(content, "\n") + "\n" + block
}

// EnsureGitAgentIgnoredAt ensures the mandatory ignore rules are in effect for
// repoRoot. It is the runtime defence for commands that create
// .git-agent/graph.db before `git-agent init` has run (capture/timeline/impact):
// without it, a first write in a freshly cloned repo leaves the database
// unignored and a later `git add -A` tracks it.
//
// It writes to .git/info/exclude (the per-repo local exclude file), NOT the
// working-tree .gitignore. Two reasons: (1) the committed .gitignore is owned
// by `git-agent init`'s Generate, which is the user-visible, shareable rules;
// (2) writing a brand-new .gitignore during a graph read would itself show up
// as an unexplained working-tree change and pollute reconcile's out-of-band
// Event Log. .git/info/exclude is local, untracked, and invisible to
// `git diff`, so it defends tracking without side effects. Idempotent — safe to
// call on every graph-db open.
func EnsureGitAgentIgnoredAt(repoRoot string) error {
	excludePath := filepath.Join(repoRoot, ".git", "info", "exclude")
	existing, _ := os.ReadFile(excludePath)
	final := EnsureMandatoryIgnoreRules(string(existing))
	if final == string(existing) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("create .git/info dir: %w", err)
	}
	return os.WriteFile(excludePath, []byte(final), 0644)
}

// gitignoreHasRule reports whether a gitignore pattern line equal to rule is
// present, ignoring blank and comment lines.
func gitignoreHasRule(content, rule string) bool {
	for _, line := range strings.Split(content, "\n") {
		if p := strings.TrimSpace(line); p != "" && !strings.HasPrefix(p, "#") && p == rule {
			return true
		}
	}
	return false
}
