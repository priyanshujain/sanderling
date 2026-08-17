package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// clauseCount is R1 to R20, the twenty clauses requirement.md numbers and the
// twenty rows review-protocol.md requires on every verdict form.
const clauseCount = 20

const (
	surfaceUnlocatable  = "unlocatable"
	surfaceInconclusive = "inconclusive"
)

const todoMarker = "todo"

// propertyMapping is one row of the property table: the clauses a property is
// the oracle for, and the locatable surfaces it reads.
type propertyMapping struct {
	Property string
	Clauses  []string
	Surfaces []string
}

// surfaceMapping says what a surface never observed across an implementation's
// whole sweep means. Only a surface the requirement obliges every
// implementation to show at all times can be read as unlocatable; a surface
// that is legitimately absent when nothing is in that state is inconclusive
// and can never mark a property unevaluated.
type surfaceMapping struct {
	Surface       string
	NeverObserved string
	Note          string
}

type mapping struct {
	Path               string
	Properties         []propertyMapping
	Surfaces           []surfaceMapping
	PropertyTodoRows   int
	byProperty         map[string]propertyMapping
	coveringProperty   map[string][]string
	unlocatableSurface map[string]bool
}

var clausePattern = regexp.MustCompile(`^[Rr]([0-9]{1,2})$`)

func canonicalClause(value string) (string, bool) {
	match := clausePattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return "", false
	}
	number := match[1]
	trimmed := strings.TrimLeft(number, "0")
	if trimmed == "" {
		return "", false
	}
	clause := "R" + trimmed
	if !slices.Contains(allClauses(), clause) {
		return "", false
	}
	return clause, true
}

func allClauses() []string {
	clauses := make([]string, 0, clauseCount)
	for index := 1; index <= clauseCount; index++ {
		clauses = append(clauses, fmt.Sprintf("R%d", index))
	}
	return clauses
}

func loadMapping(path string) (mapping, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return mapping{}, fmt.Errorf("read property-clause mapping: %w", err)
	}
	result := mapping{
		Path:               path,
		byProperty:         map[string]propertyMapping{},
		coveringProperty:   map[string][]string{},
		unlocatableSurface: map[string]bool{},
	}
	section := ""
	for _, row := range parseTableRows(string(body)) {
		head := strings.ToLower(row.cell(0))
		switch head {
		case "property":
			section = "property"
			continue
		case "surface":
			section = "surface"
			continue
		}
		switch section {
		case "property":
			if err := result.addProperty(row); err != nil {
				return mapping{}, fmt.Errorf("%s line %d: %w", path, row.Line, err)
			}
		case "surface":
			if err := result.addSurface(row); err != nil {
				return mapping{}, fmt.Errorf("%s line %d: %w", path, row.Line, err)
			}
		default:
			return mapping{}, fmt.Errorf("%s line %d: table row before any header naming property or surface", path, row.Line)
		}
	}
	if len(result.Surfaces) == 0 {
		return mapping{}, fmt.Errorf("%s declares no surfaces: a portability miss cannot be told from a clean run without them", path)
	}
	for _, property := range result.Properties {
		for _, surface := range property.Surfaces {
			if _, declared := result.surfaceByName(surface); !declared {
				return mapping{}, fmt.Errorf("%s: property %q reads surface %q, which the surface table does not declare",
					path, property.Property, surface)
			}
		}
	}
	return result, nil
}

func (m *mapping) addProperty(row tableRow) error {
	name := row.cell(0)
	if name == "" {
		return nil
	}
	if strings.EqualFold(name, todoMarker) {
		m.PropertyTodoRows++
		return nil
	}
	if _, seen := m.byProperty[name]; seen {
		return fmt.Errorf("property %q is mapped twice", name)
	}
	entry := propertyMapping{Property: name}
	for _, item := range splitList(row.cell(1)) {
		if strings.EqualFold(item, todoMarker) || item == "-" || strings.EqualFold(item, "none") {
			continue
		}
		clause, ok := canonicalClause(item)
		if !ok {
			return fmt.Errorf("property %q names clause %q, which is not one of R1 to R%d", name, item, clauseCount)
		}
		if slices.Contains(entry.Clauses, clause) {
			continue
		}
		entry.Clauses = append(entry.Clauses, clause)
	}
	for _, item := range splitList(row.cell(2)) {
		if strings.EqualFold(item, todoMarker) || item == "-" || strings.EqualFold(item, "none") {
			continue
		}
		if !slices.Contains(entry.Surfaces, item) {
			entry.Surfaces = append(entry.Surfaces, item)
		}
	}
	m.Properties = append(m.Properties, entry)
	m.byProperty[name] = entry
	for _, clause := range entry.Clauses {
		m.coveringProperty[clause] = append(m.coveringProperty[clause], name)
	}
	return nil
}

func (m *mapping) addSurface(row tableRow) error {
	name := row.cell(0)
	if name == "" || strings.EqualFold(name, todoMarker) {
		return nil
	}
	meaning := strings.ToLower(row.cell(1))
	if meaning != surfaceUnlocatable && meaning != surfaceInconclusive {
		return fmt.Errorf("surface %q says %q for never observed, want %s or %s",
			name, row.cell(1), surfaceUnlocatable, surfaceInconclusive)
	}
	if _, seen := m.surfaceByName(name); seen {
		return fmt.Errorf("surface %q is declared twice", name)
	}
	m.Surfaces = append(m.Surfaces, surfaceMapping{Surface: name, NeverObserved: meaning, Note: row.cell(2)})
	if meaning == surfaceUnlocatable {
		m.unlocatableSurface[name] = true
	}
	return nil
}

func (m mapping) surfaceByName(name string) (surfaceMapping, bool) {
	for _, surface := range m.Surfaces {
		if surface.Surface == name {
			return surface, true
		}
	}
	return surfaceMapping{}, false
}

// unevaluable reports whether a property could not be evaluated against this
// implementation because a surface it reads was never located. A surface the
// mapping calls inconclusive never makes a property unevaluable, however many
// steps failed to observe it.
func (m mapping) unevaluable(property string, observed map[string]bool) bool {
	entry, known := m.byProperty[property]
	if !known {
		return false
	}
	for _, surface := range entry.Surfaces {
		if m.unlocatableSurface[surface] && !observed[surface] {
			return true
		}
	}
	return false
}

func (m mapping) covering(clause string) []string {
	return m.coveringProperty[clause]
}

func (m mapping) knows(property string) bool {
	_, known := m.byProperty[property]
	return known
}

func (m mapping) unlocatableSurfaces() []string {
	var names []string
	for _, surface := range m.Surfaces {
		if surface.NeverObserved == surfaceUnlocatable {
			names = append(names, surface.Surface)
		}
	}
	return names
}
