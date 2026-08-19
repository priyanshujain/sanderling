package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The three models the sample is drawn from, in the capability order
// model-implementations.md fixed before any implementation was generated.
var capabilityOrder = []string{"Sonnet 5", "Opus 5", "Fable 5"}

var (
	implementationName = regexp.MustCompile(`^impl-\d+$`)
	nonAlphanumeric    = regexp.MustCompile(`[^a-z0-9]`)
)

func loadAssignment(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read assignment: %w", err)
	}
	assignments := map[string]string{}
	for _, row := range parseTableRows(string(body)) {
		name := row.cell(0)
		if !implementationName.MatchString(name) {
			continue
		}
		model, ok := canonicalModel(row.cell(1))
		if !ok {
			return nil, fmt.Errorf("%s line %d: %s is assigned model %q, which is none of %s",
				path, row.Line, name, row.cell(1), strings.Join(capabilityOrder, ", "))
		}
		if existing, seen := assignments[name]; seen && existing != model {
			return nil, fmt.Errorf("%s line %d: %s is assigned to both %s and %s",
				path, row.Line, name, existing, model)
		}
		assignments[name] = model
	}
	if len(assignments) == 0 {
		return nil, fmt.Errorf("%s maps no implementation to a model", path)
	}
	return assignments, nil
}

func canonicalModel(value string) (string, bool) {
	key := nonAlphanumeric.ReplaceAllString(strings.ToLower(value), "")
	for _, model := range capabilityOrder {
		if key == nonAlphanumeric.ReplaceAllString(strings.ToLower(model), "") {
			return model, true
		}
	}
	return "", false
}
