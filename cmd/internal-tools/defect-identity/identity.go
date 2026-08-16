package main

import (
	"fmt"
	"sort"

	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/tracecorpus"
)

// Instance is one defect as the draft identifies it across runs: the property
// that reported, the action attributed as the origin of the failed obligation,
// and the screen the witness observed. Two reports sharing all three are the
// same defect seen twice; a run-level count of violated properties cannot say
// that.
type Instance struct {
	Property      string   `json:"property"`
	OriginAction  string   `json:"origin_action"`
	WitnessScreen string   `json:"witness_screen"`
	Runs          []string `json:"runs"`
	Seeds         []int64  `json:"seeds"`
	// Reports counts violations folded into this instance, which exceeds the
	// run count only if one run reported the same property twice, and the
	// latch says it cannot.
	Reports int `json:"reports"`
}

// Unattributed is a violation that carries no origin, so the identity rule
// cannot be applied to it. It is reported rather than counted, because
// dropping it understates the defect count and guessing an origin invents one.
type Unattributed struct {
	Property string `json:"property"`
	Run      string `json:"run"`
	Step     int    `json:"step"`
	Reason   string `json:"reason"`
}

// actionKeyMode selects how much of an action two reports must share to be the
// same origin. The draft names the origin action and does not say which of its
// fields identify it, so the strict reading is available beside the default.
type actionKeyMode string

const (
	// bySelector keys an action by what it did and the name of what it did it
	// to. Coordinates and generated text differ between two runs that took the
	// same action against the same control.
	bySelector actionKeyMode = "selector"
	// byFullAction adds the text typed and the coordinates dispatched.
	byFullAction actionKeyMode = "full"
)

type identityKey struct {
	property string
	action   string
	screen   string
}

// Corpus is what one invocation read: the instances, the violations it could
// not attribute, and the counts the report needs.
type Corpus struct {
	Runs          int            `json:"runs"`
	Instances     []Instance     `json:"instances"`
	Unattributed  []Unattributed `json:"unattributed,omitempty"`
	UnnamedScreen int            `json:"unnamed_witness_screens,omitempty"`
}

// Singletons counts instances that appeared in exactly one run, the number the
// evaluation reports beside the defect count.
func (c Corpus) Singletons() int {
	count := 0
	for _, instance := range c.Instances {
		if len(instance.Runs) == 1 {
			count++
		}
	}
	return count
}

func identify(runs []tracecorpus.Run, mode actionKeyMode) (Corpus, error) {
	corpus := Corpus{Runs: len(runs)}
	byKey := map[identityKey]*Instance{}
	var order []identityKey
	for _, run := range runs {
		steps := index(run.Steps)
		for _, step := range run.Steps {
			for _, property := range step.Violations {
				witness, ok := step.Witnesses[property]
				if !ok || witness.Step == 0 {
					corpus.Unattributed = append(corpus.Unattributed, Unattributed{
						Property: property,
						Run:      run.Directory,
						Step:     step.Index,
						Reason:   attributionGap(ok),
					})
					continue
				}
				origin, ok := steps[witness.Step]
				if !ok {
					return Corpus{}, fmt.Errorf(
						"%s: %s names origin step %d, which the trace does not hold",
						run.Directory, property, witness.Step,
					)
				}
				detected, ok := steps[witness.DetectedStep]
				if !ok {
					return Corpus{}, fmt.Errorf(
						"%s: %s names detection step %d, which the trace does not hold",
						run.Directory, property, witness.DetectedStep,
					)
				}
				if detected.Screen == "" {
					corpus.UnnamedScreen++
				}
				key := identityKey{
					property: property,
					action:   actionKey(origin, mode),
					screen:   detected.Screen,
				}
				instance, seen := byKey[key]
				if !seen {
					instance = &Instance{
						Property:      property,
						OriginAction:  key.action,
						WitnessScreen: key.screen,
					}
					byKey[key] = instance
					order = append(order, key)
				}
				instance.Reports++
				if len(instance.Runs) == 0 ||
					instance.Runs[len(instance.Runs)-1] != run.Directory {
					instance.Runs = append(instance.Runs, run.Directory)
					instance.Seeds = append(instance.Seeds, run.Meta.Seed)
				}
			}
		}
	}
	for _, key := range order {
		corpus.Instances = append(corpus.Instances, *byKey[key])
	}
	sort.SliceStable(corpus.Instances, func(i, j int) bool {
		if corpus.Instances[i].Property != corpus.Instances[j].Property {
			return corpus.Instances[i].Property < corpus.Instances[j].Property
		}
		return corpus.Instances[i].OriginAction < corpus.Instances[j].OriginAction
	})
	return corpus, nil
}

func attributionGap(hasWitness bool) string {
	if !hasWitness {
		return "no witness recorded"
	}
	return "witness records no origin step"
}

func index(steps []trace.Step) map[int]trace.Step {
	byIndex := make(map[int]trace.Step, len(steps))
	for _, step := range steps {
		byIndex[step.Index] = step
	}
	return byIndex
}

// actionKey renders the action the origin step chose. The action recorded on a
// line is the one applied after observing it, which is the alignment that
// makes an origin index name an action at all.
func actionKey(origin trace.Step, mode actionKeyMode) string {
	if origin.NextAction == nil {
		return "none"
	}
	if origin.ActionSkipped != "" {
		return "none (" + origin.ActionSkipped + ")"
	}
	action := *origin.NextAction
	key := action.Kind
	switch {
	case action.Selector != "":
		key += " " + action.Selector
	case action.Key != "":
		key += " " + action.Key
	case action.X != 0 || action.Y != 0 || action.ToX != 0 || action.ToY != 0:
		key += fmt.Sprintf(" (%d,%d)", action.X, action.Y)
	}
	if mode != byFullAction {
		return key
	}
	return fmt.Sprintf("%s text=%q at=(%d,%d)->(%d,%d)",
		key, action.Text, action.X, action.Y, action.ToX, action.ToY)
}
