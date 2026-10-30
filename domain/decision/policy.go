// Package decision holds the policy shared by every System One judgment in
// git-agent.
//
// One judgment is worth making only when the code already holds the whole
// candidate set, so the model selects instead of inventing. The policy decides
// three things for every seam: whether the judgment runs at all, whether its
// answer may change the outcome, and whether the call budget still allows it.
//
// The modes exist because a judgment that has not been measured against the
// behaviour it would replace must not change that behaviour. Shadow mode runs
// the judgment and records it, then leaves the generative provider in charge.
package decision

import (
	"io"
	"reflect"
	"strings"
	"time"
)

// Mode is the operating mode of the System One layer.
type Mode string

const (
	// ModeOff runs no judgment. Every decision goes to the generative provider.
	ModeOff Mode = "off"
	// ModeShadow runs every judgment and records its answer, but the
	// generative provider still decides. Use it to measure agreement before
	// trusting a seam.
	ModeShadow Mode = "shadow"
	// ModeOn lets a confident answer decide.
	ModeOn Mode = "on"
)

// DefaultMode applies when a key is configured without an explicit mode. Shadow
// is the default because a seam has to be measured before it replaces working
// behavior.
const DefaultMode = ModeShadow

// ParseMode reads a configured mode. An empty or unknown value falls back to
// DefaultMode, so a typo leaves the layer in the safe state.
func ParseMode(s string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case ModeOff:
		return ModeOff
	case ModeOn:
		return ModeOn
	default:
		return DefaultMode
	}
}

// Defaults for the policy fields. The confidence floor covers scope assignment,
// which answered with confidence at or above 0.72 in measurement; a stricter
// judgment such as the commit type sets its own floor through the caller.
const (
	DefaultMinConfidence = 0.7
	DefaultMaxCalls      = 4
)

// Policy decides whether one judgment may run and whether it may decide.
//
// A policy is a value, so it is copied into each seam. The call counter is
// shared by every copy, so one run spends one budget no matter how many seams
// it goes through.
type Policy struct {
	Mode          Mode
	MinConfidence float64
	maxCalls      int
	used          *int
}

// NewPolicy builds a policy. A non-positive maxCalls or a confidence outside
// 0 to 1 falls back to the defaults.
func NewPolicy(mode Mode, minConfidence float64, maxCalls int) Policy {
	if minConfidence <= 0 || minConfidence > 1 {
		minConfidence = DefaultMinConfidence
	}
	if maxCalls <= 0 {
		maxCalls = DefaultMaxCalls
	}
	return Policy{Mode: mode, MinConfidence: minConfidence, maxCalls: maxCalls, used: new(int)}
}

// Enabled reports whether the judgment runs at all. Shadow mode runs it on
// purpose: an unmeasured judgment cannot be trusted to decide later.
func (p Policy) Enabled() bool { return p.Mode != ModeOff }

// Adopt reports whether an answer with this confidence may change the outcome.
func (p Policy) Adopt(confidence float64) bool {
	if p.Mode != ModeOn {
		return false
	}
	return confidence >= p.MinConfidence
}

// AdoptAt reports whether an answer may decide, using a floor for one seam. A
// seam whose answers are less reliable than scope assignment passes a stricter
// floor, and the stricter of the two wins, so a seam cannot weaken the
// configured floor.
func (p Policy) AdoptAt(confidence, floor float64) bool {
	if p.Mode != ModeOn {
		return false
	}
	if floor < p.MinConfidence {
		floor = p.MinConfidence
	}
	return confidence >= floor
}

// Take reserves one call from the budget. It reports false once the budget is
// spent, which lets a caller skip the judgment instead of slowing a commit down.
func (p *Policy) Take() bool {
	if !p.Enabled() || p.Used() >= p.maxCalls {
		return false
	}
	*p.used++
	return true
}

// Spent reports whether the budget is exhausted.
func (p Policy) Spent() bool { return !p.Enabled() || p.Used() >= p.maxCalls }

// MaxCalls reports the run's call budget.
func (p Policy) MaxCalls() int { return p.maxCalls }

// Used reports how many calls this run has reserved.
func (p Policy) Used() int {
	if p.used == nil {
		return 0
	}
	return *p.used
}

// Agreement names how a judgment compared with the decision it shadowed.
type Agreement string

const (
	AgreementNone     Agreement = "none"  // no baseline to compare with
	AgreementAgree    Agreement = "agree" // the judgment and the baseline agree
	AgreementDisagree Agreement = "disagree"
)

// Observation records one judgment. Recording is the only way a shadow-mode run
// teaches anything, so a seam that judges and does not record is a seam nobody
// can promote to ModeOn on evidence.
type Observation struct {
	Seam        string // stable seam name, e.g. "scope_set"
	Mode        Mode
	Adopted     bool   // the judgment changed the outcome
	Decision    string // what the judgment answered, in code-owned terms
	Baseline    string // what the generative provider answered
	Agreement   Agreement
	Confidence  float64
	Latency     time.Duration
	InputTokens int
	Err         string
}

// Recorder receives one observation per judgment.
type Recorder interface {
	Record(Observation)
}

// RecorderFunc adapts a function to Recorder.
type RecorderFunc func(Observation)

// Record implements Recorder.
func (f RecorderFunc) Record(o Observation) { f(o) }

// MultiRecorder forwards to each recorder, skipping nil ones.
type MultiRecorder []Recorder

// Record implements Recorder.
func (m MultiRecorder) Record(o Observation) {
	for _, r := range m {
		if r != nil {
			r.Record(o)
		}
	}
}

// NilWriter reports whether a writer cannot be used. An io.Writer holding a nil
// pointer is not equal to nil, so a plain comparison lets a nil buffer through
// and panics on the first write.
func NilWriter(w io.Writer) bool {
	if w == nil {
		return true
	}
	v := reflect.ValueOf(w)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// DiscardRecorder drops every observation. Use it when no writer is wired.
func DiscardRecorder() Recorder { return RecorderFunc(func(Observation) {}) }

// NewRecorder returns a recorder that writes one line per observation, which is
// the shape an operator reads in verbose output. It returns a discarding
// recorder for a nil writer.
func NewRecorder(w io.Writer) Recorder {
	if NilWriter(w) {
		return DiscardRecorder()
	}
	return RecorderFunc(func(o Observation) {
		line := o.Seam + " " + string(o.Mode)
		if o.Confidence > 0 {
			line += fmtConfidence(o.Confidence)
		}
		if o.Decision != "" {
			line += " decided=" + o.Decision
		}
		if o.Baseline != "" && o.Baseline != o.Decision {
			line += " baseline=" + o.Baseline
		}
		switch o.Agreement {
		case AgreementAgree:
			line += " agreement=agree"
		case AgreementDisagree:
			line += " agreement=disagree"
		}
		if o.Adopted {
			line += " adopted=true"
		}
		if o.Latency > 0 {
			line += " latency=" + o.Latency.Round(time.Millisecond).String()
		}
		if o.InputTokens > 0 {
			line += " in_tokens=" + itoa(o.InputTokens)
		}
		if o.Err != "" {
			line += " error=" + o.Err
		}
		w.Write([]byte(line + "\n"))
	})
}

func fmtConfidence(c float64) string {
	return " confidence=" + itoa(int(c*100+0.5))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
