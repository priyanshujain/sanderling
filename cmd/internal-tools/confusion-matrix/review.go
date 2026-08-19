package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	clauseMeets      = "meets"
	clauseViolates   = "violates"
	clauseCannotTell = "cannot tell"
)

const (
	overallDefective    = "defective"
	overallNotDefective = "not defective"
)

// reviewVerdict is one filed verdict form: the twenty clause rows and the
// overall verdict review-protocol.md requires, with any adjudicated label
// already substituted for the first rater's.
type reviewVerdict struct {
	Implementation string
	Path           string
	Reviewer       string
	Date           string
	Minutes        int
	Overall        string
	Clauses        map[string]string
	Adjudicated    []string
}

func (v reviewVerdict) defective() bool { return v.Overall == overallDefective }

func (v reviewVerdict) violatedClauses() []string {
	var violated []string
	for _, clause := range allClauses() {
		if v.Clauses[clause] == clauseViolates {
			violated = append(violated, clause)
		}
	}
	return violated
}

type malformedReview struct {
	Implementation string
	Path           string
	Reason         string
}

type reviewSide struct {
	Directory string
	Verdicts  []reviewVerdict
	Malformed []malformedReview
}

var (
	reviewFileName      = regexp.MustCompile(`^(impl-\d+)\.md$`)
	adjudicationName    = regexp.MustCompile(`^(impl-\d+)-adjudication\.md$`)
	secondRaterName     = regexp.MustCompile(`^impl-\d+-r2\.md$`)
	keyValueLine        = regexp.MustCompile(`(?i)^\s*[-*]?\s*(reviewer|date|minutes|overall verdict|overall)\s*:\s*(.+?)\s*$`)
	leadingWholeNumbers = regexp.MustCompile(`^\d+`)
)

func loadReviews(directory string) (reviewSide, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return reviewSide{}, fmt.Errorf("read reviews: %w", err)
	}
	side := reviewSide{Directory: directory}
	adjudications := map[string]map[string]string{}
	var forms []struct {
		name string
		path string
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		switch {
		case secondRaterName.MatchString(entry.Name()):
			continue
		case adjudicationName.MatchString(entry.Name()):
			name := adjudicationName.FindStringSubmatch(entry.Name())[1]
			resolved, err := readAdjudication(path)
			if err != nil {
				side.Malformed = append(side.Malformed, malformedReview{
					Implementation: name, Path: path, Reason: err.Error(),
				})
				continue
			}
			adjudications[name] = resolved
		case reviewFileName.MatchString(entry.Name()):
			forms = append(forms, struct {
				name string
				path string
			}{reviewFileName.FindStringSubmatch(entry.Name())[1], path})
		}
	}

	for _, form := range forms {
		verdict, err := readReview(form.name, form.path)
		if err != nil {
			side.Malformed = append(side.Malformed, malformedReview{
				Implementation: form.name, Path: form.path, Reason: err.Error(),
			})
			continue
		}
		for clause, label := range adjudications[form.name] {
			if verdict.Clauses[clause] != label {
				verdict.Adjudicated = append(verdict.Adjudicated, clause)
			}
			verdict.Clauses[clause] = label
		}
		slices.Sort(verdict.Adjudicated)
		side.Verdicts = append(side.Verdicts, verdict)
	}
	slices.SortFunc(side.Verdicts, func(a, b reviewVerdict) int {
		return strings.Compare(a.Implementation, b.Implementation)
	})
	slices.SortFunc(side.Malformed, func(a, b malformedReview) int {
		return strings.Compare(a.Path, b.Path)
	})
	return side, nil
}

func readReview(name, path string) (reviewVerdict, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return reviewVerdict{}, err
	}
	verdict := reviewVerdict{Implementation: name, Path: path, Clauses: map[string]string{}}
	for _, line := range strings.Split(string(body), "\n") {
		match := keyValueLine.FindStringSubmatch(normalizeCell(line))
		if match == nil {
			continue
		}
		value := strings.TrimSpace(match[2])
		switch strings.ToLower(match[1]) {
		case "reviewer":
			verdict.Reviewer = value
		case "date":
			verdict.Date = value
		case "minutes":
			if digits := leadingWholeNumbers.FindString(value); digits != "" {
				verdict.Minutes, _ = strconv.Atoi(digits)
			}
		case "overall", "overall verdict":
			overall, ok := canonicalOverall(value)
			if !ok {
				return reviewVerdict{}, fmt.Errorf("overall verdict %q is neither %s nor %s", value, overallDefective, overallNotDefective)
			}
			verdict.Overall = overall
		}
	}
	clauses, err := readClauseRows(string(body))
	if err != nil {
		return reviewVerdict{}, err
	}
	verdict.Clauses = clauses
	for _, clause := range allClauses() {
		if _, filed := verdict.Clauses[clause]; !filed {
			return reviewVerdict{}, fmt.Errorf("no row for clause %s: the form must carry all %d", clause, clauseCount)
		}
	}
	if verdict.Overall == "" {
		return reviewVerdict{}, fmt.Errorf("no overall verdict: the matrix scores the reviewer's own %s or %s", overallDefective, overallNotDefective)
	}
	return verdict, nil
}

func readClauseRows(body string) (map[string]string, error) {
	clauses := map[string]string{}
	for _, row := range parseTableRows(body) {
		clause, ok := canonicalClause(row.cell(0))
		if !ok {
			continue
		}
		label, ok := canonicalClauseVerdict(row.cell(1))
		if !ok {
			return nil, fmt.Errorf("clause %s has verdict %q, want %s, %s or %s",
				clause, row.cell(1), clauseMeets, clauseViolates, clauseCannotTell)
		}
		if existing, seen := clauses[clause]; seen && existing != label {
			return nil, fmt.Errorf("clause %s is filed twice, as %q and %q", clause, existing, label)
		}
		clauses[clause] = label
	}
	return clauses, nil
}

func readAdjudication(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	resolved, err := readClauseRows(string(body))
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no resolved clause rows")
	}
	return resolved, nil
}

func canonicalClauseVerdict(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case clauseMeets, "meet", "met":
		return clauseMeets, true
	case clauseViolates, "violate", "violated":
		return clauseViolates, true
	case clauseCannotTell, "cannot-tell", "cannot_tell", "can't tell", "cant tell":
		return clauseCannotTell, true
	default:
		return "", false
	}
}

func canonicalOverall(value string) (string, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	cleaned = strings.TrimSuffix(cleaned, ".")
	switch cleaned {
	case overallDefective:
		return overallDefective, true
	case overallNotDefective, "not-defective", "no defect", "clean":
		return overallNotDefective, true
	default:
		return "", false
	}
}
