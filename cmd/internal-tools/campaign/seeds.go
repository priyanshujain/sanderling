package main

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSeeds expands a seed specification such as "1-10,20,30-32" into the
// explicit seed list a campaign intends to run.
func parseSeeds(specification string) ([]int64, error) {
	trimmed := strings.TrimSpace(specification)
	if trimmed == "" {
		return nil, fmt.Errorf("empty seed spec")
	}
	var seeds []int64
	seen := map[int64]bool{}
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty seed in %q", specification)
		}
		expanded, err := expandSeedPart(part)
		if err != nil {
			return nil, err
		}
		for _, seed := range expanded {
			if seen[seed] {
				return nil, fmt.Errorf("duplicate seed %d in %q", seed, specification)
			}
			seen[seed] = true
			seeds = append(seeds, seed)
		}
	}
	return seeds, nil
}

func expandSeedPart(part string) ([]int64, error) {
	start, end, isRange := strings.Cut(part, "-")
	if !isRange {
		seed, err := parseSeed(part)
		if err != nil {
			return nil, err
		}
		return []int64{seed}, nil
	}
	first, err := parseSeed(strings.TrimSpace(start))
	if err != nil {
		return nil, fmt.Errorf("seed range %q: %w", part, err)
	}
	last, err := parseSeed(strings.TrimSpace(end))
	if err != nil {
		return nil, fmt.Errorf("seed range %q: %w", part, err)
	}
	if first > last {
		return nil, fmt.Errorf("seed range %q: start %d is above end %d", part, first, last)
	}
	seeds := make([]int64, 0, last-first+1)
	for seed := first; seed <= last; seed++ {
		seeds = append(seeds, seed)
	}
	return seeds, nil
}

func parseSeed(text string) (int64, error) {
	seed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid seed %q: want a positive integer", text)
	}
	if seed == 0 {
		// `sanderling test` reads --seed 0 as "derive a seed from the clock",
		// so a campaign listing seed 0 records a run nobody can reproduce.
		return 0, fmt.Errorf("seed 0 is not reproducible: sanderling test derives a random seed when --seed is 0, so list explicit non-zero seeds")
	}
	if seed < 0 {
		return 0, fmt.Errorf("invalid seed %d: want a positive integer", seed)
	}
	return seed, nil
}
