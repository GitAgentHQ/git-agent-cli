package application

import (
	"io"
	"time"

	"github.com/gitagenthq/git-agent/domain/decision"
)

// Decisions is the System One layer shared by every judgment in one command
// run. It carries the policy that decides whether a judgment runs and whether
// it may decide, plus the recorder that makes a shadow run measurable.
//
// A nil *Decisions means the layer is off. Every service must treat it that way,
// so a caller that does not wire the layer keeps the generative path.
type Decisions struct {
	Policy   decision.Policy
	Recorder decision.Recorder
	// Log receives the human-readable form of each observation. It is the
	// verbose output the operator reads, and it is nil when verbose is off.
	Log io.Writer
	// misses is shared by every session of one run, like the call budget. A
	// private map per session would never accumulate, and the seam breaker
	// would never fire.
	misses map[string]int
}

// NewDecisions builds a layer from a mode, a confidence floor, a per-run call
// budget, a recorder, and a log writer. It returns nil when the mode is off, so
// the caller cannot accidentally run judgments in the off mode.
func NewDecisions(mode decision.Mode, minConfidence float64, maxCalls int, recorder decision.Recorder, log io.Writer) *Decisions {
	if mode == decision.ModeOff {
		return nil
	}
	if recorder == nil {
		recorder = decision.NewRecorder(log)
	}
	return &Decisions{
		Policy:   decision.NewPolicy(mode, minConfidence, maxCalls),
		Recorder: recorder,
		Log:      log,
		misses:   map[string]int{},
	}
}

// Enabled reports whether any judgment may run.
func (d *Decisions) Enabled() bool {
	return d != nil && d.Policy.Enabled()
}

// Session is one command run's view of the layer. It hands each seam its own
// copy of the policy, so a seam cannot spend another seam's budget.
type Session struct {
	policy   decision.Policy
	recorder decision.Recorder
	// misses counts, per seam, how many judgments in this run fell short of the
	// floor. It is what stops a seam that cannot reach its floor from being
	// called again and again in one run.
	misses map[string]int
}

// SeamMissLimit is how many short judgments one run tolerates per seam before
// the seam is skipped. Two is enough to tell a hard seam from a borderline one
// without giving up on a single unlucky answer.
const SeamMissLimit = 2

// Begin returns a session for one command run.
func (d *Decisions) Begin() *Session {
	if d == nil {
		return nil
	}
	return &Session{policy: d.Policy, recorder: d.Recorder, misses: d.misses}
}

// Take reserves one call from the run's budget and reports whether the seam may
// run. Reserving before the call is what keeps two seams from both running on
// the last remaining call.
//
// A seam that already missed its floor twice in this run is skipped. In the on
// mode a miss means the answer was not confident enough to act, and a third ask
// in the same run will not change that; paying for it would only slow the
// commit. In shadow mode a miss is the expected outcome, so nothing is skipped
// and the comparison keeps running.
func (s *Session) Take(seam string) bool {
	return s.reserve(seam)
}

// blocked reports whether a seam has run out of chances in this run.
func (s *Session) blocked(seam string) bool {
	return s.policy.Mode == decision.ModeOn && s.misses[seam] >= SeamMissLimit
}

// Take reserves the call before checking the breaker, so a blocked seam spends
// nothing.
func (s *Session) reserve(seam string) bool {
	if s == nil || s.blocked(seam) {
		return false
	}
	return s.policy.Take()
}

// note records how one judgment turned out, so a seam that keeps missing its
// floor stops being called. The counter is shared by every session of the run,
// so the misses of one seam accumulate across the whole command.
func (s *Session) note(seam string, adopted bool) {
	if s == nil || adopted || s.misses == nil {
		return
	}
	s.misses[seam]++
}

// Adopt reports whether an answer with this confidence may change the outcome,
// and records the result for the seam. A shadow session always answers false,
// which is what keeps an unmeasured judgment from replacing working behavior.
func (s *Session) Adopt(seam string, confidence float64) bool {
	if s == nil {
		return false
	}
	adopted := s.policy.Adopt(confidence)
	s.note(seam, adopted)
	return adopted
}

// AdoptAt reports whether an answer may decide against a stricter seam floor,
// and records the result for the seam.
func (s *Session) AdoptAt(seam string, confidence, floor float64) bool {
	if s == nil {
		return false
	}
	adopted := s.policy.AdoptAt(confidence, floor)
	s.note(seam, adopted)
	return adopted
}

// Mode reports the session's mode, for the observation record.
func (s *Session) Mode() decision.Mode {
	if s == nil {
		return decision.ModeOff
	}
	return s.policy.Mode
}

// Report records one observation. Timing is measured around the call by the
// caller, which is the only place that knows both ends of it.
//
// The recorder is the only output: Decisions builds it from the log writer, so
// a caller never has to decide where an observation goes.
func (s *Session) Report(started time.Time, o decision.Observation) {
	if s == nil || s.recorder == nil {
		return
	}
	o.Mode = s.Mode()
	o.Latency = time.Since(started)
	s.recorder.Record(o)
}

// Seam names are the stable identifiers a recorder groups by.
const (
	SeamScopeSet  = "scope_set"
	SeamTechStack = "tech_stack"
	SeamGrouping  = "grouping"
	SeamHookRoute = "hook_route"
	SeamTypeScope = "type_scope"
)
