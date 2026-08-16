package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeCorpus builds a tree with the shape the sweep expects: every example
// directory the population was drawn from, each holding the document that
// implementation is served at.
func writeCorpus(t *testing.T, names []string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, examplesDirectory, name), 0o755); err != nil {
			t.Fatal(err)
		}
		document := filepath.Join(root, filepath.FromSlash(documentFor(name)))
		if err := os.MkdirAll(filepath.Dir(document), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "<!doctype html><title>" + name + "</title><ul class=\"todo-list\"></ul>"
		if err := os.WriteFile(document, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func wholeCorpus(t *testing.T) string {
	t.Helper()
	return writeCorpus(
		t,
		slices.Concat(includedImplementations, excludedImplementations),
	)
}

func TestPopulation_IsFortyThreeNamesDisjointFromTheExclusions(t *testing.T) {
	if len(includedImplementations) != 43 {
		t.Errorf(
			"population size: got %d, want 43",
			len(includedImplementations),
		)
	}
	seen := map[string]bool{}
	for _, name := range includedImplementations {
		if seen[name] {
			t.Errorf("%q appears twice in the population", name)
		}
		seen[name] = true
	}
	for _, name := range excludedImplementations {
		if seen[name] {
			t.Errorf("%q is both included and excluded", name)
		}
	}
}

func TestVerifyCorpus_RejectsATreeThatIsNotThisCorpus(t *testing.T) {
	if err := verifyCorpus(wholeCorpus(t)); err != nil {
		t.Fatalf(
			"the corpus this population was drawn from should verify: %v",
			err,
		)
	}

	short := writeCorpus(
		t,
		slices.Concat(includedImplementations[1:], excludedImplementations),
	)
	err := verifyCorpus(short)
	if err == nil {
		t.Fatal(
			"a corpus missing an implementation swept 42 arms and reported them as 43",
		)
	}
	if !strings.Contains(err.Error(), includedImplementations[0]) {
		t.Errorf("the error should name what is missing: %v", err)
	}

	extra := writeCorpus(
		t,
		slices.Concat(
			includedImplementations,
			excludedImplementations,
			[]string{"svelte"},
		),
	)
	err = verifyCorpus(extra)
	if err == nil {
		t.Fatal(
			"a corpus with an implementation this population never drew from verified",
		)
	}
	if !strings.Contains(err.Error(), "svelte") {
		t.Errorf("the error should name what is unexpected: %v", err)
	}
}

func TestDocumentFor_ServesTheOverriddenPathForImplementationsWithoutARootIndex(
	t *testing.T,
) {
	for name, want := range map[string]string{
		"angular-dart": "examples/angular-dart/web/index.html",
		"duel":         "examples/duel/www/index.html",
		"vanillajs":    "examples/vanillajs/index.html",
		"react":        "examples/react/index.html",
	} {
		if got := documentFor(name); got != want {
			t.Errorf("%s document: got %q, want %q", name, got, want)
		}
	}
}

func TestPlanImplementations_RefusesAnImplementationWhoseDocumentIsMissing(
	t *testing.T,
) {
	root := wholeCorpus(t)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(documentFor("duel")))); err != nil {
		t.Fatal(err)
	}
	_, err := planImplementations(root, []string{"duel"}, 5400)
	if err == nil {
		t.Fatal(
			"an implementation whose document is missing would be swept as a 404 page",
		)
	}
	if !strings.Contains(err.Error(), "duel") {
		t.Errorf("the error should name the implementation: %v", err)
	}
}

func TestSelectImplementations_DefaultsToThePopulationAndRejectsNamesOutsideIt(
	t *testing.T,
) {
	all, err := selectImplementations("")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(all, includedImplementations) {
		t.Errorf(
			"an empty selection should be the whole population, got %d names",
			len(all),
		)
	}
	subset, err := selectImplementations("react, angular2_es2015")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(subset, []string{"react", "angular2_es2015"}) {
		t.Errorf("subset: got %v", subset)
	}
	for _, selection := range []string{"cujo", "svelte", "react,react"} {
		if _, err := selectImplementations(selection); err == nil {
			t.Errorf("selection %q should have been rejected", selection)
		}
	}
}
