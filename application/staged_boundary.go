package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gitagenthq/git-agent/domain/diff"
	pkgerrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// The index is the one split git-agent acts on.
//
// Everything here follows from that rule. A change the user staged is the boundary
// the user drew, so the run commits exactly that content first, from the diff the
// index holds. It writes no index entry, rebuilds no content, and invents no
// boundary. Every other split belongs to the user, and the run's job is to say
// when one is possible.

const (
	maxRegionHeadings      = 3
	maxDetailedRegionFiles = 3
)

// commitStagedContent commits what the index holds, exactly as it holds it, and
// returns that commit. It returns nil when the index holds nothing.
//
// The message describes the staged diff, not the file, and goes through the same
// filter and the same size caps as any other diff. Hooks validate it like any
// other commit. A dry run reports it without making it.
func (s *CommitService) commitStagedContent(ctx context.Context, req CommitRequest) (*SingleCommitResult, error) {
	staged, err := s.git.StagedDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("staged diff: %w", err)
	}
	if len(staged.Files) == 0 {
		return nil, nil
	}
	if len(staged.Content) == 0 {
		return nil, fmt.Errorf("the index reports %d staged file(s) but no diff", len(staged.Files))
	}

	genDiff, err := s.preparedForModel(ctx, req, staged, "the staged content")
	if err != nil {
		return nil, err
	}
	attempt, err := s.generateGroupMessage(ctx, req, genDiff, staged, 1, 1, "")
	if err != nil {
		return nil, err
	}
	if !attempt.passed {
		return nil, &HookBlockedError{LastMessage: attempt.preTrailer, Reason: attempt.hookFeedback}
	}

	result := &SingleCommitResult{
		Title:       attempt.msg.Title,
		Message:     attempt.preTrailer,
		Explanation: attempt.msg.Explanation,
		Files:       staged.Files,
		HookOutcome: attempt.hookOutcome,
	}
	if req.DryRun {
		return result, nil
	}
	gitOut, err := s.git.Commit(ctx, attempt.assembled)
	if err != nil {
		if errors.Is(err, pkgerrors.ErrNothingToCommit) {
			// The index emptied between the read and the commit, which only a
			// concurrent process can do.
			return nil, nil
		}
		return nil, err
	}
	result.GitOutput = gitOut
	if hash, hashErr := s.git.CommitHash(ctx); hashErr == nil {
		result.SHA = hash
	}
	return result, nil
}

// remainderDiff returns the change left over once the index content is accounted
// for: the working tree measured against the index, plus every path git does not
// track yet.
//
// The first part is what makes a partly staged file work. The working tree still
// holds the region the user did not stage, and measuring it against the index
// reports exactly that region and nothing the user already staged.
//
// A path whose only difference from HEAD is the index is excluded, because the
// staged commit already accounts for it. In a real run those paths are committed
// by then and never appear here. In a dry run the index is left alone, and without
// this the preview would report the staged path twice: once as the staged commit
// and once as a file the file-level flow would stage whole. stagedPaths carries
// what the index held, so the exclusion holds on both paths.
func (s *CommitService) remainderDiff(ctx context.Context, stagedPaths []string) (*diff.StagedDiff, error) {
	remainder, err := s.git.UnstagedDiff(ctx)
	if err != nil {
		return nil, fmt.Errorf("unstaged diff: %w", err)
	}
	all, err := s.git.AllChangedFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}
	handled := make(map[string]bool, len(stagedPaths))
	for _, file := range stagedPaths {
		handled[file] = true
	}
	seen := make(map[string]bool, len(remainder.Files)+len(all))
	for _, file := range remainder.Files {
		seen[file] = true
	}
	for _, file := range all {
		if seen[file] || handled[file] {
			continue
		}
		seen[file] = true
		remainder.Files = append(remainder.Files, file)
	}
	return remainder, nil
}

type regionReport struct {
	file     string
	headings []string
}

// reportSplittableFiles tells the user when a file the run is about to commit as a
// whole holds more than one region, and names the regions so the user can decide.
//
// This is what a tool can honestly say. Where a commit boundary belongs is the
// user's judgment and the index is how they express it; all the run knows is that
// git reported several regions in one file, which is the fact that makes a split
// possible. A small run names each file on the warning stream. A larger run keeps
// one bounded line there, with a concrete command, and writes every per-file
// report to the verbose stream. It reports and never splits.
func (s *CommitService) reportSplittableFiles(req CommitRequest, remainder *diff.StagedDiff) {
	if remainder == nil || remainder.Content == "" {
		return
	}
	regions := diffRegions(remainder.Content)
	files := make([]string, 0, len(regions))
	for file := range regions {
		files = append(files, file)
	}
	sort.Strings(files)

	reports := make([]regionReport, 0, len(files))
	for _, file := range files {
		headings := regions[file]
		if len(headings) < 2 {
			continue
		}
		reports = append(reports, regionReport{file: file, headings: headings})
	}
	if len(reports) == 0 {
		return
	}
	if len(reports) <= maxDetailedRegionFiles {
		for _, report := range reports {
			s.warn(req, "Warning: %s holds %d separate regions (%s): stage them apart with \"git add -p %s\" to commit them separately",
				report.file, len(report.headings), describeRegions(report.headings), report.file)
		}
		return
	}

	examples := make([]string, 0, maxDetailedRegionFiles)
	for _, report := range reports[:maxDetailedRegionFiles] {
		examples = append(examples, report.file)
	}
	hint := "; run with --verbose to list every file"
	if req.Verbose {
		hint = ""
	}
	s.warn(req, "Warning: %d files hold separate regions (%s, and %d more): stage one region at a time with \"git add -p %s\" to commit them separately%s",
		len(reports), strings.Join(examples, ", "), len(reports)-maxDetailedRegionFiles, reports[0].file, hint)
	for _, report := range reports {
		s.vlog(req, "Warning: %s holds %d separate regions (%s): stage them apart with \"git add -p %s\" to commit them separately",
			report.file, len(report.headings), describeRegions(report.headings), report.file)
	}
}

// diffRegions reads a diff's hunk headers per file: the heading git anchored each
// region to, one entry per region, in the order git printed them.
func diffRegions(content string) map[string][]string {
	regions := map[string][]string{}
	file := ""
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			if i := strings.IndexByte(file, '\t'); i >= 0 {
				file = file[:i]
			}
			if file != "" {
				if _, seen := regions[file]; !seen {
					regions[file] = nil
				}
			}
		case strings.HasPrefix(line, "@@"):
			if file == "" {
				continue
			}
			regions[file] = append(regions[file], regionHeading(line))
		}
	}
	return regions
}

// regionHeading reads the text git prints after a hunk header, which for most
// languages names the function or type the region sits in. It is empty when the
// diff carries no anchor, which is not a reason to hide the region: `git add -p`
// splits such a file too.
func regionHeading(line string) string {
	rest := line[2:]
	end := strings.Index(rest, "@@")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[end+2:])
}

// describeRegions names the regions for the report, up to maxRegionHeadings.
func describeRegions(headings []string) string {
	named := dedupeStrings(headings, maxRegionHeadings)
	if len(named) == 0 {
		return "no heading in the diff"
	}
	return strings.Join(named, ", ")
}

// warn writes a line the user has to see. A terminal run and a scripted run both
// receive it: progress lines may be dropped when no terminal is attached, but a
// warning that explains what the run did not do must not share that rule.
func (s *CommitService) warn(req CommitRequest, format string, args ...any) {
	writer := req.WarnWriter
	if writer == nil {
		writer = req.OutWriter
	}
	if writer != nil {
		fmt.Fprintf(writer, format+"\n", args...)
	}
}

// stagedBoundaryPlanInputs is the plan input for the file-level flow that follows
// the staged commit. The index holds nothing by then, so that flow sees no staged
// files and the remainder as ordinary unstaged work.
func stagedBoundaryPlanInputs(remainder *diff.StagedDiff) (*diff.StagedDiff, *diff.StagedDiff) {
	return &diff.StagedDiff{}, &diff.StagedDiff{Files: remainder.Files}
}
