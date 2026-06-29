package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/gitagenthq/git-agent/domain/graph"
	agentErrors "github.com/gitagenthq/git-agent/pkg/errors"
)

// Candidate is one ranked suspect Event within the Suspect Window.
type Candidate struct {
	Seq        int64    `json:"seq"`
	EventID    string   `json:"event_id"`
	Tool       string   `json:"tool"`
	Files      []string `json:"files"`
	BeforeBlob string   `json:"before_blob"`
	AfterBlob  string   `json:"after_blob"`
	Score      float64  `json:"score"`
}

// DiagnosisResult is the forensic output of graph diagnose.
type DiagnosisResult struct {
	GreenSeq      int64       `json:"green_seq"` // last-green Outcome Event seq (0 if none)
	RedSeq        int64       `json:"red_seq"`   // first-red Outcome Event seq
	WindowSize    int         `json:"window_size"`
	Candidates    []Candidate `json:"candidates"` // ranked, total order
	ChainVerified bool        `json:"chain_verified"`
	LowConfidence string      `json:"low_confidence"` // e.g. "no_green_baseline", empty when high-confidence
	Warnings      []string    `json:"warnings"`       // non-fatal degradations, e.g. an LLM re-rank that fell back to deterministic order
}

// DiagnoseRequest carries the symptom and diagnose options.
type DiagnoseRequest struct {
	Symptom string
	Files   []string // --file seeds
	UseLLM  bool     // --llm
	Force   bool     // --force: proceed despite a ChainBreak
	TopN    int      // LLM re-rank bound
}

// DiagnoseReranker reorders, but never extends, the deterministic top-N.
type DiagnoseReranker interface {
	Rerank(ctx context.Context, symptom string, candidates []Candidate) ([]Candidate, error)
}

// DiagnoseService traces a symptom to the Candidate Event that most likely
// introduced it, over the Suspect Window between last-green and first-red Outcome
// Events. Steps 1-4 are deterministic; step 5 (LLM re-rank) only reorders the
// deterministic top-N.
type DiagnoseService struct {
	repo   graph.GraphRepository
	impact *ImpactService
	llm    DiagnoseReranker // bounded re-rank port; nil/no-endpoint => deterministic order is final
}

// NewDiagnoseService creates a DiagnoseService over the Event Log repository, the
// co-change ImpactService (read-only reuse), and an optional bounded reranker.
func NewDiagnoseService(repo graph.GraphRepository, impact *ImpactService, llm DiagnoseReranker) *DiagnoseService {
	return &DiagnoseService{repo: repo, impact: impact, llm: llm}
}

// diagnoseScoring holds the architecture.md weights as one place to read them.
var diagnoseScoring = struct {
	recency, impactOverlap, directSeedHit, churn, laterReverted float64
}{
	recency:       0.35,
	impactOverlap: 0.25,
	directSeedHit: 0.25,
	churn:         0.10,
	laterReverted: 0.05,
}

// inferredRedPenalty down-weights a window whose first-red exit code was inferred
// rather than reported (best-practices.md pitfall 13).
const inferredRedPenalty = 0.5

// Diagnose runs the deterministic pipeline (verify -> window -> relevant set ->
// score) and the optional bounded LLM re-rank.
func (s *DiagnoseService) Diagnose(ctx context.Context, req DiagnoseRequest) (*DiagnosisResult, error) {
	// 1. Verify the chain first; refuse (exit 4) on a break unless --force.
	vr, err := s.repo.VerifyChain(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify chain: %w", err)
	}
	chainOK := vr.Status == "ok"
	if !chainOK && !req.Force {
		return nil, agentErrors.ErrChainIntegrity
	}

	// Load the full Event Log in seq order.
	events, err := s.streamAll(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Suspect Window from Outcome Events matching the symptom's test.
	lastGreen, firstRed, redEvent := suspectWindow(events, req.Symptom)
	result := &DiagnosisResult{
		ChainVerified: chainOK,
		GreenSeq:      lastGreen,
	}
	if firstRed == 0 {
		// No matching red Outcome Event: nothing to diagnose.
		return result, nil
	}
	result.RedSeq = firstRed
	if lastGreen == 0 {
		result.LowConfidence = "no_green_baseline"
	}

	windowEvents := windowBetween(events, lastGreen, firstRed)

	// 3. Relevant file set R: seeds expanded by co-change impact (depth 1).
	relevant, seeds := s.relevantSet(ctx, req, windowEvents)

	// 4. Candidates = window Events touching R, scored deterministically.
	candidates := s.scoreCandidates(ctx, windowEvents, relevant, seeds, lastGreen, firstRed, redEvent)
	result.WindowSize = len(candidates)
	result.Candidates = candidates

	// 5. Optional bounded LLM re-rank over the top-N only. A re-rank failure is
	// non-fatal: the deterministic candidates are already final, so the result
	// degrades to deterministic order with a warning rather than failing the
	// whole command. The LLM may reorder but never add candidates.
	if req.UseLLM && s.llm != nil && len(candidates) > 0 {
		reranked, err := s.rerank(ctx, req, candidates)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("llm re-rank failed, using deterministic order: %v", err))
		} else {
			result.Candidates = reranked
		}
	}

	return result, nil
}

// streamAll reads the whole Event Log into memory in seq order.
func (s *DiagnoseService) streamAll(ctx context.Context) ([]graph.EventRecord, error) {
	cur, err := s.repo.StreamEvents(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("stream events: %w", err)
	}
	defer cur.Close()
	var events []graph.EventRecord
	for cur.Next() {
		events = append(events, cur.Event())
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return events, nil
}

// suspectWindow finds last_green = MAX(seq) green test outcome matching the
// symptom, and first_red = MIN(seq) > last_green red outcome for the same test.
// With no green baseline, lastGreen is 0 (window opens at Genesis).
func suspectWindow(events []graph.EventRecord, symptom string) (lastGreen, firstRed int64, redEvent graph.EventRecord) {
	for _, e := range events {
		if !isMatchingTestOutcome(e, symptom) {
			continue
		}
		if *e.ExitCode == 0 {
			if e.Seq > lastGreen {
				lastGreen = e.Seq
			}
		}
	}
	for _, e := range events {
		if !isMatchingTestOutcome(e, symptom) {
			continue
		}
		if *e.ExitCode != 0 && e.Seq > lastGreen {
			if firstRed == 0 || e.Seq < firstRed {
				firstRed = e.Seq
				redEvent = e
			}
		}
	}
	return lastGreen, firstRed, redEvent
}

// isMatchingTestOutcome reports whether e is a test Outcome Event whose test
// matches the symptom. A blank symptom matches any test outcome.
func isMatchingTestOutcome(e graph.EventRecord, symptom string) bool {
	if e.Kind != graph.EventKindOutcome || !e.IsTest || e.ExitCode == nil {
		return false
	}
	if symptom == "" {
		return true
	}
	return e.TestName == symptom
}

// windowBetween returns the Events strictly between lastGreen and firstRed in seq
// order. lastGreen == 0 opens the window at Genesis.
func windowBetween(events []graph.EventRecord, lastGreen, firstRed int64) []graph.EventRecord {
	var out []graph.EventRecord
	for _, e := range events {
		if e.Seq > lastGreen && e.Seq < firstRed {
			out = append(out, e)
		}
	}
	return out
}

// relevantSet computes the relevant file set R: the seeds (from req.Files plus
// the files touched in the window) expanded by co-change impact at depth 1. It
// returns R and the direct seed set.
func (s *DiagnoseService) relevantSet(ctx context.Context, req DiagnoseRequest, windowEvents []graph.EventRecord) (relevant, seeds map[string]bool) {
	seeds = map[string]bool{}
	for _, f := range req.Files {
		if f != "" {
			seeds[f] = true
		}
	}
	relevant = map[string]bool{}
	for f := range seeds {
		relevant[f] = true
	}

	seedList := make([]string, 0, len(seeds))
	for f := range seeds {
		seedList = append(seedList, f)
	}
	sort.Strings(seedList)

	if len(seedList) > 0 {
		res, err := s.impact.Impact(ctx, graph.ImpactRequest{Paths: seedList, Depth: 1})
		if err == nil && res != nil {
			for _, e := range res.CoChanged {
				relevant[e.Path] = true
			}
		}
	}
	return relevant, seeds
}

// scoreCandidates scores every window Event touching the relevant set, resolving
// renames so a touch on a prior path still counts. The order is total, ties
// broken by higher seq.
func (s *DiagnoseService) scoreCandidates(ctx context.Context, windowEvents []graph.EventRecord, relevant, seeds map[string]bool, lastGreen, firstRed int64, redEvent graph.EventRecord) []Candidate {
	span := float64(firstRed - lastGreen)
	if span <= 0 {
		span = 1
	}
	inferredRed := redEvent.ExitCodeSource == "inferred"

	// Prefetch every window file's blob refs in one query, indexed by (seq, file),
	// so per-Event scoring never re-queries FileChanges (avoiding an O(events x
	// files) scan of the same rows).
	blobIdx := s.buildBlobIndex(ctx, windowEvents)

	var candidates []Candidate
	for _, e := range windowEvents {
		files := s.eventFiles(ctx, e)
		if len(files) == 0 {
			continue
		}
		overlap, directHit := s.overlap(ctx, files, relevant, seeds)
		if overlap == 0 && !directHit {
			continue
		}

		recency := float64(e.Seq-lastGreen) / span
		impactOverlap := float64(overlap) / float64(len(files))
		directSeed := 0.0
		if directHit {
			directSeed = 1.0
		}
		churn := churnScore(e)
		reverted := laterReverted(blobIdx, e, files, windowEvents)

		score := diagnoseScoring.recency*recency +
			diagnoseScoring.impactOverlap*impactOverlap +
			diagnoseScoring.directSeedHit*directSeed +
			diagnoseScoring.churn*churn -
			diagnoseScoring.laterReverted*reverted
		if inferredRed {
			score *= inferredRedPenalty
		}

		before, after := candidateBlobs(blobIdx, e, files, seeds)
		candidates = append(candidates, Candidate{
			Seq:        e.Seq,
			EventID:    e.EventID,
			Tool:       e.ToolName,
			Files:      files,
			BeforeBlob: before,
			AfterBlob:  after,
			Score:      score,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Seq > candidates[j].Seq
	})
	return candidates
}

// eventFiles returns the sorted set of files an Event touched, from its payload.
func (s *DiagnoseService) eventFiles(_ context.Context, e graph.EventRecord) []string {
	changes := extractFileChanges(e)
	files := make([]string, 0, len(changes))
	for _, c := range changes {
		files = append(files, c.path)
	}
	sort.Strings(files)
	return files
}

// overlap counts how many of the Event's files are in the relevant set R
// (rename-resolved), and whether any is a direct seed hit.
func (s *DiagnoseService) overlap(ctx context.Context, files []string, relevant, seeds map[string]bool) (count int, directHit bool) {
	for _, f := range files {
		identities := append([]string{f}, s.resolveIdentities(ctx, f)...)
		inR, isSeed := false, false
		for _, id := range identities {
			if relevant[id] {
				inR = true
			}
			if seeds[id] {
				isSeed = true
			}
		}
		if inR {
			count++
		}
		if isSeed {
			directHit = true
		}
	}
	return count, directHit
}

// resolveIdentities returns the file's prior/renamed identities, swallowing
// errors (a missing rename resolution simply means no extra identities).
func (s *DiagnoseService) resolveIdentities(ctx context.Context, file string) []string {
	priors, err := s.repo.ResolveRenames(ctx, file)
	if err != nil {
		return nil
	}
	return priors
}

// churnScore is the Event's line churn squashed into [0,1].
func churnScore(e graph.EventRecord) float64 {
	total := 0
	for _, c := range extractFileChanges(e) {
		total += c.additions + c.deletions
	}
	if total <= 0 {
		return 0
	}
	return float64(total) / float64(total+10)
}

// blobRef is the before/after File Blob Refs for one (seq, file) event_files row.
type blobRef struct{ before, after string }

// buildBlobIndex fetches every window file's event_files rows in a single
// FileChanges query and indexes them by (seq, file), so scoring reads blobs from
// memory instead of re-querying per Event.
func (s *DiagnoseService) buildBlobIndex(ctx context.Context, windowEvents []graph.EventRecord) map[string]blobRef {
	idx := make(map[string]blobRef)

	seen := make(map[string]bool)
	var files []string
	for _, e := range windowEvents {
		for _, f := range s.eventFiles(ctx, e) {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		return idx
	}

	rows, err := s.repo.FileChanges(ctx, files)
	if err != nil {
		return idx
	}
	for _, r := range rows {
		idx[blobKey(r.Seq, r.FilePath)] = blobRef{before: r.BeforeBlob, after: r.AfterBlob}
	}
	return idx
}

func blobKey(seq int64, file string) string {
	return file + "\x00" + strconv.FormatInt(seq, 10)
}

// fileBlobsFor returns the before/after blobs recorded for (seq, file), empty
// when the index has no such row.
func fileBlobsFor(idx map[string]blobRef, seq int64, file string) (before, after string) {
	r := idx[blobKey(seq, file)]
	return r.before, r.after
}

// laterReverted is 1 when a later window Event on the same file undoes this
// Event (its after_blob returns to this Event's before_blob), else 0.
func laterReverted(idx map[string]blobRef, e graph.EventRecord, files []string, windowEvents []graph.EventRecord) float64 {
	for _, f := range files {
		before, _ := fileBlobsFor(idx, e.Seq, f)
		if before == "" {
			continue
		}
		for _, later := range windowEvents {
			if later.Seq <= e.Seq {
				continue
			}
			_, after := fileBlobsFor(idx, later.Seq, f)
			if after != "" && after == before {
				return 1
			}
		}
	}
	return 0
}

// candidateBlobs returns the before/after File Blob Refs for the Event,
// preferring a seed file's row when one is present.
func candidateBlobs(idx map[string]blobRef, e graph.EventRecord, files []string, seeds map[string]bool) (before, after string) {
	// Prefer a directly-seeded file's blobs for the candidate diff.
	for _, f := range files {
		if seeds[f] {
			if b, a := fileBlobsFor(idx, e.Seq, f); a != "" || b != "" {
				return b, a
			}
		}
	}
	for _, f := range files {
		if b, a := fileBlobsFor(idx, e.Seq, f); a != "" || b != "" {
			return b, a
		}
	}
	return "", ""
}

// rerank passes only the deterministic top-N to the LLM and intersects the
// returned order back against the deterministic set, so the LLM may reorder
// within N but can never add candidates.
func (s *DiagnoseService) rerank(ctx context.Context, req DiagnoseRequest, candidates []Candidate) ([]Candidate, error) {
	topN := req.TopN
	if topN <= 0 || topN > len(candidates) {
		topN = len(candidates)
	}
	head := candidates[:topN]
	tail := candidates[topN:]

	allowed := make(map[int64]Candidate, len(head))
	for _, c := range head {
		allowed[c.Seq] = c
	}

	reranked, err := s.llm.Rerank(ctx, req.Symptom, head)
	if err != nil {
		return nil, err
	}

	ordered := make([]Candidate, 0, len(candidates))
	seen := make(map[int64]bool, len(head))
	for _, c := range reranked {
		orig, ok := allowed[c.Seq]
		if !ok || seen[c.Seq] {
			continue // forged or duplicate candidate: drop it.
		}
		seen[c.Seq] = true
		ordered = append(ordered, orig)
	}
	// Append any top-N candidate the LLM omitted, preserving the deterministic
	// order, so the set is never shrunk either.
	for _, c := range head {
		if !seen[c.Seq] {
			ordered = append(ordered, c)
		}
	}
	return append(ordered, tail...), nil
}
