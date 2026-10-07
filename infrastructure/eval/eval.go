// Package eval measures a System One seam against real repository history.
//
// A judgment is only worth trusting after it has been compared with the behavior
// it would replace. This package builds that comparison: it reads commits from a
// repository, reconstructs the structured state each seam would see at run time,
// and reports how often the judgment agrees with the recorded commit message.
//
// The ground truth has two halves and they answer different questions. The human
// title says what the change was. The provider title says what git-agent does
// today. A seam that disagrees with the human title may still be an improvement
// over the provider, and one that agrees with the provider changes nothing.
package eval

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sample is one commit prepared for judging.
type Sample struct {
	SHA string
	// Title is the recorded commit subject, which carries both the human
	// prefix and, for generated commits, the provider's choice.
	Title string
	// Files is the changed path list, one per line.
	Files []string
	// Numstat is the raw git numstat output for the commit.
	Numstat string
	// Diff is the change body, capped at MaxSampleDiffBytes so a huge commit
	// cannot blow up a measurement run.
	Diff string
	// DiffSize is the byte size of the full diff, recorded to show how much
	// of it a judgment would have to leave behind.
	DiffSize int
	// Date orders the samples the way history does.
	Date time.Time
}

// MaxSampleDiffBytes caps the diff a sample keeps. It matches the way the CLI
// truncates a change before planning, so a measurement sees the same input the
// provider would.
const MaxSampleDiffBytes = 120_000

// ConventionalPrefix splits a commit title into its type and scope, which is the
// form a type and scope judgment answers in.
func ConventionalPrefix(title string) (commitType, scope string, ok bool) {
	head, _, _ := strings.Cut(title, ":")
	head = strings.TrimSpace(head)
	// A breaking-change marker sits between the prefix and the colon, as in
	// "refactor(cli)!:".
	head = strings.TrimSuffix(head, "!")
	open := strings.Index(head, "(")
	if !strings.HasSuffix(head, ")") || open < 0 {
		if head == "" || strings.ContainsAny(head, " \t") {
			return "", "", false
		}
		return strings.ToLower(head), "", true
	}
	commitType = strings.ToLower(strings.TrimSpace(head[:open]))
	scope = strings.ToLower(strings.TrimSpace(head[open+1 : len(head)-1]))
	if commitType == "" || scope == "" {
		return "", "", false
	}
	return commitType, scope, true
}

// Collector reads samples from a git repository.
type Collector struct {
	// Dir is the repository to read. An empty value uses the current directory.
	Dir string
}

// Collect returns up to limit samples, newest first, skipping merge commits
// because a merge title describes no single change.
func (c Collector) Collect(ctx context.Context, limit int) ([]Sample, error) {
	out, err := c.git(ctx, "log", "--no-merges", "-n", strconv.Itoa(limit),
		"--pretty=format:%H\x1f%s\x1f%cI")
	if err != nil {
		return nil, fmt.Errorf("reading commit log: %w", err)
	}

	var samples []Sample
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\x1f")
		if len(fields) != 3 {
			continue
		}
		date, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("parsing commit date %q: %w", fields[2], err)
		}
		sample := Sample{SHA: fields[0], Title: fields[1], Date: date}
		if sample.Files, err = c.gitLines(ctx, "show", "--format=", "--name-only", sample.SHA); err != nil {
			return nil, fmt.Errorf("reading files of %s: %w", sample.SHA, err)
		}
		if sample.Numstat, err = c.git(ctx, "show", "--format=", "--numstat", sample.SHA); err != nil {
			return nil, fmt.Errorf("reading numstat of %s: %w", sample.SHA, err)
		}
		diff, err := c.git(ctx, "show", "--format=", "--unified=3", sample.SHA)
		if err != nil {
			return nil, fmt.Errorf("reading diff of %s: %w", sample.SHA, err)
		}
		sample.DiffSize = len(diff)
		if len(diff) > MaxSampleDiffBytes {
			diff = diff[:MaxSampleDiffBytes]
		}
		sample.Diff = diff
		samples = append(samples, sample)
	}
	return samples, nil
}

func (c Collector) git(ctx context.Context, args ...string) (string, error) {
	out, err := c.run(ctx, args...)
	return strings.TrimRight(out, "\n"), err
}

func (c Collector) gitLines(ctx context.Context, args ...string) ([]string, error) {
	out, err := c.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines, nil
}

func (c Collector) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out), nil
}

// Report is the outcome of one measured seam.
type Report struct {
	Seam string
	// Samples is how many commits were judged.
	Samples int
	// Judged is how many produced a usable answer.
	Judged int
	// TypeHits, ScopeHits, and PrefixHits count agreement with the recorded
	// title. PrefixHits counts an exact "type(scope)" match.
	TypeHits, ScopeHits, PrefixHits int
	// TypeMisses and ScopeMisses hold the recorded titles that disagreed, so a
	// reviewer can adjudicate a disagreement instead of trusting the count.
	TypeMisses  []string
	ScopeMisses []string
	// TypeMissConfidences and ScopeMissConfidences hold the confidence each
	// disagreement happened at. They are what a confidence floor has to be
	// checked against: a floor only means something when the wrong answers
	// arrive below it.
	TypeMissConfidences  []float64
	ScopeMissConfidences []float64
	// Confidence is the mean confidence over the judged samples.
	Confidence float64
	// TypeConfidence and ScopeConfidence are the mean of each part separately.
	// A part is gated on its own floor, so the harness has to report them apart:
	// one report number would hide which part can be trusted.
	TypeConfidence  float64
	ScopeConfidence float64
	// Latency is the mean wall time of one judgment.
	Latency time.Duration
	// InputTokens is the total state size sent.
	InputTokens int
	// DiffBytesTotal is what the judgments deliberately left behind.
	DiffBytesTotal int
}

// ScopeFloorHolds reports whether every scope disagreement arrived below floor,
// which is the only condition under which pinning a scope above that floor is
// safe. A disagreement above the floor means the confidence carries no signal
// for that part.
func (r Report) ScopeFloorHolds(floor float64) bool {
	for _, confidence := range r.ScopeMissConfidences {
		if confidence >= floor {
			return false
		}
	}
	return true
}

// TypeFloorHolds reports whether every type disagreement arrived below floor.
// Measurement found it does not: the wrong types arrive highly confident, so no
// floor makes the type safe to pin.
func (r Report) TypeFloorHolds(floor float64) bool {
	for _, confidence := range r.TypeMissConfidences {
		if confidence >= floor {
			return false
		}
	}
	return true
}

// Accuracy returns the share of judged samples whose whole prefix matched.
func (r Report) Accuracy() float64 {
	if r.Judged == 0 {
		return 0
	}
	return float64(r.PrefixHits) / float64(r.Judged)
}

// String renders the report as one readable block.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "seam %s: %d samples, %d judged\n", r.Seam, r.Samples, r.Judged)
	fmt.Fprintf(&b, "  prefix accuracy : %.0f%% (%d of %d)\n", r.Accuracy()*100, r.PrefixHits, r.Judged)
	fmt.Fprintf(&b, "  type agreement  : %.0f%%\n", percent(r.TypeHits, r.Judged))
	fmt.Fprintf(&b, "  scope agreement : %.0f%%\n", percent(r.ScopeHits, r.Judged))
	fmt.Fprintf(&b, "  mean confidence : %.2f (type %.2f, scope %.2f)\n", r.Confidence, r.TypeConfidence, r.ScopeConfidence)
	fmt.Fprintf(&b, "  mean latency    : %s\n", r.Latency.Round(time.Millisecond))
	fmt.Fprintf(&b, "  state sent      : %d tokens\n", r.InputTokens)
	fmt.Fprintf(&b, "  diff left out   : %.1f MiB\n", float64(r.DiffBytesTotal)/(1<<20))
	if len(r.TypeMisses) > 0 {
		fmt.Fprintf(&b, "  type misses     : %s\n", strings.Join(r.TypeMisses, "; "))
	}
	if len(r.ScopeMisses) > 0 {
		fmt.Fprintf(&b, "  scope misses    : %s\n", strings.Join(r.ScopeMisses, "; "))
	}
	return b.String()
}

func percent(hits, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(hits) / float64(of) * 100
}

// Record folds one judged sample into a report.
func (r *Report) Record(s Sample, wantType, wantScope, gotType, gotScope string, confidence float64, latency time.Duration, inputTokens int) {
	r.RecordParts(s, wantType, wantScope, gotType, gotScope, confidence, confidence, confidence, latency, inputTokens)
}

// RecordParts folds one judged sample into a report, keeping the two parts of a
// prefix apart. typeConfidence and scopeConfidence are what a caller would gate
// each part on, so measuring them together with the outcome is what shows
// whether a floor means anything for that part.
func (r *Report) RecordParts(s Sample, wantType, wantScope, gotType, gotScope string, confidence, typeConfidence, scopeConfidence float64, latency time.Duration, inputTokens int) {
	r.Samples++
	r.Judged++
	r.Confidence += confidence
	r.TypeConfidence += typeConfidence
	r.ScopeConfidence += scopeConfidence
	r.Latency += latency
	r.InputTokens += inputTokens
	r.DiffBytesTotal += s.DiffSize

	if gotType == wantType {
		r.TypeHits++
	} else {
		r.TypeMisses = append(r.TypeMisses, fmt.Sprintf("%s want %s got %s at %.2f",
			shortSHA(s.SHA), wantType, gotType, typeConfidence))
		r.TypeMissConfidences = append(r.TypeMissConfidences, typeConfidence)
	}
	if wantScope == "" {
		// A commit with no scope carries no scope to get right.
		r.ScopeHits++
	} else if gotScope == wantScope {
		r.ScopeHits++
	} else {
		r.ScopeMisses = append(r.ScopeMisses, fmt.Sprintf("%s want %s got %s at %.2f",
			shortSHA(s.SHA), wantScope, gotScope, scopeConfidence))
		r.ScopeMissConfidences = append(r.ScopeMissConfidences, scopeConfidence)
	}
	if gotType == wantType && gotScope == wantScope {
		r.PrefixHits++
	}
}

// Finish averages the accumulated sums. It is safe to call once after the last
// Record.
func (r *Report) Finish() {
	if r.Judged > 0 {
		r.Confidence /= float64(r.Judged)
		r.TypeConfidence /= float64(r.Judged)
		r.ScopeConfidence /= float64(r.Judged)
		r.Latency /= time.Duration(r.Judged)
	}
	sort.Strings(r.TypeMisses)
	sort.Strings(r.ScopeMisses)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ParseNumstat reads git numstat output into per-file counts. A binary file row
// carries "-" instead of a count and contributes nothing.
func ParseNumstat(raw string) map[string][2]int {
	out := map[string][2]int{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 3 {
			continue
		}
		adds, errA := strconv.Atoi(strings.TrimSpace(fields[0]))
		dels, errD := strconv.Atoi(strings.TrimSpace(fields[1]))
		if errA != nil || errD != nil {
			continue
		}
		out[fields[2]] = [2]int{adds, dels}
	}
	return out
}
