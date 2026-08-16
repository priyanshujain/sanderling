package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion"
	"github.com/priyanshujain/sanderling/internal/tracecorpus"
)

// observation is one hierarchy-bearing step: the index the run gave it and the
// state it observed.
type observation struct {
	Step  int
	State string
}

// Reach is what one run explored. Distinct counts the structural states its
// observations visited, which is the measure; Observations is how many looks
// it took to visit them.
type Reach struct {
	Directory    string `json:"directory"`
	Seed         int64  `json:"seed"`
	Platform     string `json:"platform"`
	Arm          string `json:"arm,omitempty"`
	Observations int    `json:"observations"`
	Distinct     int    `json:"distinct_states"`
	// Unobserved counts steps carrying no hierarchy, which the run-end
	// finalize record is, so an observation count cannot be read as a step
	// count by accident.
	Unobserved   int `json:"steps_without_hierarchy"`
	observations []observation
}

// measure keys each observation by a digest of its structural hash. The
// measure is equality between hashes and nothing else, and a corpus holds
// thousands of trees whose hashes run to tens of kilobytes each.
func measure(run tracecorpus.Run) Reach {
	reach := Reach{
		Directory: run.Directory,
		Seed:      run.Meta.Seed,
		Platform:  run.Meta.Platform,
		Arm:       run.Meta.Arm,
	}
	distinct := map[string]bool{}
	for _, step := range run.Steps {
		if step.Hierarchy == nil {
			reach.Unobserved++
			continue
		}
		digest := sha256.Sum256([]byte(ioscompanion.StructuralHash(step.Hierarchy)))
		key := hex.EncodeToString(digest[:])
		reach.observations = append(
			reach.observations,
			observation{Step: step.Index, State: key},
		)
		distinct[key] = true
	}
	reach.Observations = len(reach.observations)
	reach.Distinct = len(distinct)
	return reach
}

// corpusDistinct counts the structural states the whole corpus reached, which
// is not the sum of the per-run counts: runs of one application revisit the
// same screens.
func corpusDistinct(reaches []Reach) int {
	distinct := map[string]bool{}
	for _, reach := range reaches {
		for _, seen := range reach.observations {
			distinct[seen.State] = true
		}
	}
	return len(distinct)
}

// Divergence is where a replay stopped observing what the reference observed.
// Step is the index of the first observation whose structural state differs;
// Diverged is false when the replay matched the reference for every
// observation the two share, in which case Step is that shared length and the
// observation is right-censored.
type Divergence struct {
	Directory string `json:"directory"`
	Seed      int64  `json:"seed"`
	Step      int    `json:"step"`
	Diverged  bool   `json:"diverged"`
	Compared  int    `json:"observations_compared"`
}

func diverge(reference, replay Reach) Divergence {
	result := Divergence{Directory: replay.Directory, Seed: replay.Seed}
	shared := len(reference.observations)
	if len(replay.observations) < shared {
		shared = len(replay.observations)
	}
	result.Compared = shared
	for position := 0; position < shared; position++ {
		if reference.observations[position].State != replay.observations[position].State {
			result.Step = reference.observations[position].Step
			result.Diverged = true
			return result
		}
	}
	if shared > 0 {
		result.Step = reference.observations[shared-1].Step
	}
	return result
}

// medianDivergence is E6's number: the median observation index at which a
// replay first diverges from the reference. A replay that never diverged
// enters at the last index the two share, which is where the observation is
// censored rather than where it broke, so the count of such replays is
// reported beside the median rather than folded into it.
func medianDivergence(divergences []Divergence) (median float64, censored int) {
	if len(divergences) == 0 {
		return 0, 0
	}
	steps := make([]int, 0, len(divergences))
	for _, divergence := range divergences {
		steps = append(steps, divergence.Step)
		if !divergence.Diverged {
			censored++
		}
	}
	sort.Ints(steps)
	middle := len(steps) / 2
	if len(steps)%2 == 1 {
		return float64(steps[middle]), censored
	}
	return float64(steps[middle-1]+steps[middle]) / 2, censored
}
