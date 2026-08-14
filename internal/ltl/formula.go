package ltl

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Formula is the AST of a temporal logic property.
type Formula interface {
	isFormula()
	describe() string
}

// PredicateLabel lets a ThunkFormula expose the caller's label for the closure
// it wraps. ThunkFormula satisfies it through the name passed to ThunkNamed;
// an unnamed thunk serializes without a name.
type PredicateLabel interface {
	PredicateName() string
}

// ErrorFormula represents a thunk that threw during evaluation. The verifier
// substitutes one of these into the residual when MarshalJSON would otherwise
// have to encode an opaque thunk that already errored. It exists so that the
// replay UI can render "predicate threw" inline.
type ErrorFormula struct {
	Message string
}

func (ErrorFormula) isFormula() {}
func (e ErrorFormula) describe() string {
	return fmt.Sprintf("Error(%q)", e.Message)
}

// AlwaysFormula obliges its inner formula to hold at every step. A bounded
// Always (the dual of a bounded Eventually) holds for the steps inside its
// window and is vacuously satisfied once the window closes. An unbounded
// Always carries no bound fields and is checked at every observed step.
type AlwaysFormula struct {
	Inner                Formula
	StepBound            int
	HasStepBound         bool
	ExpiryObservation    int
	HasExpiryObservation bool
	Duration             time.Duration
	Deadline             time.Time
	HasDeadline          bool
}

type PureFormula struct {
	Value bool
}

// ThunkFormula wraps an opaque predicate closure. The closure returns the
// predicate's boolean result and a non-nil error when the predicate threw; a
// thrown predicate is a witnessed violation distinct from a plain false.
//
// Every thunk carries an identity assigned at construction, and the identity
// is part of its describe() key. Two thunks are therefore equal keys only when
// they are copies of the same constructed value, which is what lets obligation
// collapse merge residuals without ever merging distinct predicates. The
// fields are unexported so a thunk cannot be built without one.
type ThunkFormula struct {
	predicate func() (bool, error)
	name      string
	identity  uint64
}

func (t ThunkFormula) PredicateName() string { return t.name }

// thunkIdentities hands out the per-thunk identity. It only has to separate
// thunks within one process, so a counter is enough.
var thunkIdentities atomic.Uint64

// NowFormula marks its inner formula for evaluation at the current step only.
// Primarily used so that now(...).implies(...) parses unambiguously.
type NowFormula struct {
	Inner Formula
}

// NextFormula obliges its inner formula to hold at the next step (not this one).
type NextFormula struct {
	Inner Formula
}

// EventuallyFormula obliges its inner formula to hold at some step within the
// given bound. An unbounded eventually never triggers a violation within a
// finite run.
//
// When Duration is non-zero and Deadline is the zero time, the evaluator
// resolves the absolute deadline on first reduction using the observation
// time. This matches the "within N seconds of obligation instantiation"
// semantics used by nested Always(Eventually(...).within(...)) formulas.
//
// StepBound is the step-domain counterpart: the window counts observations the
// evaluator reduced, and ExpiryObservation is the absolute closing observation
// the evaluator resolves on first reduction, exactly as Deadline is for
// Duration.
type EventuallyFormula struct {
	Inner                Formula
	StepBound            int
	HasStepBound         bool
	ExpiryObservation    int
	HasExpiryObservation bool
	Duration             time.Duration
	Deadline             time.Time
	HasDeadline          bool
}

type ImpliesFormula struct {
	Antecedent Formula
	Consequent Formula
}

type OrFormula struct {
	Left  Formula
	Right Formula
}

type AndFormula struct {
	Left  Formula
	Right Formula
}

type NotFormula struct {
	Inner Formula
}

func Always(inner Formula) Formula { return AlwaysFormula{Inner: inner} }

func Pure(value bool) Formula { return PureFormula{Value: value} }

func Thunk(function func() (bool, error)) Formula {
	return ThunkFormula{predicate: function, identity: thunkIdentities.Add(1)}
}

func ThunkNamed(name string, function func() (bool, error)) Formula {
	return ThunkFormula{
		predicate: function,
		name:      name,
		identity:  thunkIdentities.Add(1),
	}
}

func Now(inner Formula) Formula { return NowFormula{Inner: inner} }

func Next(inner Formula) Formula { return NextFormula{Inner: inner} }

func Eventually(inner Formula) Formula { return EventuallyFormula{Inner: inner} }

func EventuallyWithinSteps(inner Formula, steps int) Formula {
	return EventuallyFormula{Inner: inner, StepBound: steps, HasStepBound: true}
}

func EventuallyBefore(inner Formula, deadline time.Time) Formula {
	return EventuallyFormula{Inner: inner, Deadline: deadline, HasDeadline: true}
}

func EventuallyWithin(inner Formula, duration time.Duration) Formula {
	return EventuallyFormula{Inner: inner, Duration: duration}
}

func Implies(antecedent, consequent Formula) Formula {
	return ImpliesFormula{Antecedent: antecedent, Consequent: consequent}
}

func Or(left, right Formula) Formula { return OrFormula{Left: left, Right: right} }

func And(left, right Formula) Formula { return AndFormula{Left: left, Right: right} }

func Not(inner Formula) Formula { return NotFormula{Inner: inner} }

func (AlwaysFormula) isFormula()     {}
func (PureFormula) isFormula()       {}
func (ThunkFormula) isFormula()      {}
func (NowFormula) isFormula()        {}
func (NextFormula) isFormula()       {}
func (EventuallyFormula) isFormula() {}
func (ImpliesFormula) isFormula()    {}
func (OrFormula) isFormula()         {}
func (AndFormula) isFormula()        {}
func (NotFormula) isFormula()        {}

func (a AlwaysFormula) describe() string {
	parts := []string{a.Inner.describe()}
	if a.HasStepBound {
		parts = append(parts, fmt.Sprintf("steps=%d", a.StepBound))
	}
	if a.HasExpiryObservation {
		parts = append(parts, fmt.Sprintf("expiresAtObservation=%d", a.ExpiryObservation))
	}
	if a.HasDeadline {
		parts = append(parts, "deadline="+a.Deadline.Format(time.RFC3339Nano))
	} else if a.Duration > 0 {
		parts = append(parts, "within="+a.Duration.String())
	}
	return "Always(" + strings.Join(parts, ", ") + ")"
}
func (p PureFormula) describe() string { return fmt.Sprintf("Pure(%t)", p.Value) }
func (t ThunkFormula) describe() string {
	return fmt.Sprintf("Thunk(%s#%d)", t.name, t.identity)
}
func (n NowFormula) describe() string  { return "Now(" + n.Inner.describe() + ")" }
func (n NextFormula) describe() string { return "Next(" + n.Inner.describe() + ")" }
func (e EventuallyFormula) describe() string {
	parts := []string{e.Inner.describe()}
	if e.HasStepBound {
		parts = append(parts, fmt.Sprintf("steps=%d", e.StepBound))
	}
	if e.HasExpiryObservation {
		parts = append(parts, fmt.Sprintf("expiresAtObservation=%d", e.ExpiryObservation))
	}
	if e.HasDeadline {
		parts = append(parts, "deadline="+e.Deadline.Format(time.RFC3339Nano))
	} else if e.Duration > 0 {
		parts = append(parts, "within="+e.Duration.String())
	}
	return "Eventually(" + strings.Join(parts, ", ") + ")"
}
func (i ImpliesFormula) describe() string {
	return "Implies(" + i.Antecedent.describe() + ", " + i.Consequent.describe() + ")"
}
func (o OrFormula) describe() string {
	return "Or(" + o.Left.describe() + ", " + o.Right.describe() + ")"
}
func (a AndFormula) describe() string {
	return "And(" + a.Left.describe() + ", " + a.Right.describe() + ")"
}
func (n NotFormula) describe() string { return "Not(" + n.Inner.describe() + ")" }

// Describe returns a debug-friendly representation of the formula.
func Describe(formula Formula) string { return formula.describe() }

// withinNode mirrors the optional `within` clause attached to bounded
// Always/Eventually nodes in the JSON AST.
type withinNode struct {
	Amount int64  `json:"amount"`
	Unit   string `json:"unit"`
	// Deadline is the absolute instant the window closes, in unix
	// milliseconds, present once the evaluator resolved a relative duration
	// against an observation. Two obligations spawned at different steps from
	// the same duration differ only here, so without it they serialize
	// identically and the trace erases the distinction the evaluator makes.
	Deadline int64 `json:"deadline,omitempty"`
	// ExpiresAtObservation is the step-domain counterpart of Deadline: the
	// index of the observation the window closes at. It names observations
	// rather than runner steps because a step the verifier skipped never
	// reached the evaluator and so cannot close a window; the pair of fields
	// is what lets a reader tell the two numberings apart.
	ExpiresAtObservation int `json:"expiresAtObservation,omitempty"`
}

// boundWindow is the optional window shared by AlwaysFormula and
// EventuallyFormula: the window the spec authored plus the absolute close the
// evaluator resolved for this obligation.
type boundWindow struct {
	hasStepBound         bool
	stepBound            int
	hasExpiryObservation bool
	expiryObservation    int
	duration             time.Duration
	hasDeadline          bool
	deadline             time.Time
}

// withinFor renders the bound clause of a bounded Always or Eventually. The
// authored window (steps or duration) stays in amount/unit so readers keep
// seeing what the spec asked for; the resolved close rides alongside.
func withinFor(window boundWindow) *withinNode {
	switch {
	case window.hasStepBound:
		node := &withinNode{Amount: int64(window.stepBound), Unit: "steps"}
		if window.hasExpiryObservation {
			node.ExpiresAtObservation = window.expiryObservation
		}
		return node
	case window.duration > 0:
		node := &withinNode{Amount: window.duration.Milliseconds(), Unit: "milliseconds"}
		if window.hasDeadline {
			node.Deadline = window.deadline.UnixMilli()
		}
		return node
	case window.hasDeadline:
		return &withinNode{Amount: window.deadline.UnixMilli(), Unit: "deadline"}
	default:
		return nil
	}
}

func (a AlwaysFormula) boundWindow() boundWindow {
	return boundWindow{
		hasStepBound:         a.HasStepBound,
		stepBound:            a.StepBound,
		hasExpiryObservation: a.HasExpiryObservation,
		expiryObservation:    a.ExpiryObservation,
		duration:             a.Duration,
		hasDeadline:          a.HasDeadline,
		deadline:             a.Deadline,
	}
}

func (e EventuallyFormula) boundWindow() boundWindow {
	return boundWindow{
		hasStepBound:         e.HasStepBound,
		stepBound:            e.StepBound,
		hasExpiryObservation: e.HasExpiryObservation,
		expiryObservation:    e.ExpiryObservation,
		duration:             e.Duration,
		hasDeadline:          e.HasDeadline,
		deadline:             e.Deadline,
	}
}

func (a AlwaysFormula) MarshalJSON() ([]byte, error) {
	payload := struct {
		Op     string      `json:"op"`
		Arg    Formula     `json:"arg"`
		Within *withinNode `json:"within,omitempty"`
	}{Op: "always", Arg: a.Inner}
	payload.Within = withinFor(a.boundWindow())
	return json.Marshal(payload)
}

func (n NowFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op  string  `json:"op"`
		Arg Formula `json:"arg"`
	}{"now", n.Inner})
}

func (n NextFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op  string  `json:"op"`
		Arg Formula `json:"arg"`
	}{"next", n.Inner})
}

func (n NotFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op  string  `json:"op"`
		Arg Formula `json:"arg"`
	}{"not", n.Inner})
}

func (e EventuallyFormula) MarshalJSON() ([]byte, error) {
	payload := struct {
		Op     string      `json:"op"`
		Arg    Formula     `json:"arg"`
		Within *withinNode `json:"within,omitempty"`
	}{Op: "eventually", Arg: e.Inner}
	payload.Within = withinFor(e.boundWindow())
	return json.Marshal(payload)
}

func (a AndFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op    string  `json:"op"`
		Left  Formula `json:"left"`
		Right Formula `json:"right"`
	}{"and", a.Left, a.Right})
}

func (o OrFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op    string  `json:"op"`
		Left  Formula `json:"left"`
		Right Formula `json:"right"`
	}{"or", o.Left, o.Right})
}

func (i ImpliesFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op    string  `json:"op"`
		Left  Formula `json:"left"`
		Right Formula `json:"right"`
	}{"implies", i.Antecedent, i.Consequent})
}

func (p PureFormula) MarshalJSON() ([]byte, error) {
	if p.Value {
		return []byte(`{"op":"true"}`), nil
	}
	return []byte(`{"op":"false"}`), nil
}

func (t ThunkFormula) MarshalJSON() ([]byte, error) {
	payload := struct {
		Op   string `json:"op"`
		Name string `json:"name,omitempty"`
	}{Op: "predicate"}
	if labeled, ok := any(t).(PredicateLabel); ok {
		payload.Name = labeled.PredicateName()
	}
	return json.Marshal(payload)
}

func (e ErrorFormula) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Op      string `json:"op"`
		Message string `json:"message"`
	}{"error", e.Message})
}
