package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The corpus is tastejs/todomvc. Its examples/ directory holds 48
// implementations of one requirement; five are excluded and the 43 that remain
// are this experiment's population.
//
// The population is named here rather than read from whatever directories
// happen to exist, so a corpus at the wrong commit fails verifyCorpus instead
// of quietly sweeping a different sample and reporting it as this one.
var includedImplementations = []string{
	"angular-dart", "angular2", "angular2_es2015", "angularjs", "angularjs_require",
	"aurelia", "backbone", "backbone_marionette", "backbone_require", "binding-scala",
	"canjs", "canjs_require", "closure", "dijon", "dojo",
	"duel", "elm", "emberjs", "enyo_backbone", "exoskeleton",
	"jquery", "js_of_ocaml", "jsblocks", "knockback", "knockoutjs",
	"knockoutjs_require", "kotlin-react", "lavaca_require", "mithril", "polymer",
	"ractive", "react", "react-alt", "react-backbone", "reagent",
	"riotjs", "scalajs-react", "typescript-angular", "typescript-backbone", "typescript-react",
	"vanilla-es6", "vanillajs", "vue",
}

// excludedImplementations are the five directories under examples/ that the
// corpus survey dropped. They are listed rather than merely omitted so
// verifyCorpus can insist the corpus holds exactly these 48 names: an example
// added or renamed upstream then stops the sweep rather than silently shrinking
// or growing the sample.
var excludedImplementations = []string{
	"cujo", "emberjs_require", "firebase-angular", "gwt", "react-hooks",
}

const examplesDirectory = "examples"

// documentPath names the served document for the implementations that do not
// keep an index.html at the root of their example directory.
var documentPath = map[string]string{
	"angular-dart": "examples/angular-dart/web/index.html",
	"duel":         "examples/duel/www/index.html",
}

func documentFor(name string) string {
	if override, ok := documentPath[name]; ok {
		return override
	}
	return examplesDirectory + "/" + name + "/index.html"
}

// implementation is one member of the population, with the port it owns for the
// whole sweep. The port is what keeps implementations apart: every one is
// served on its own, so every one is its own web origin and localStorage keeps
// their records in separate partitions. Four pairs in this corpus write the
// same key, and one origin between them is one record between them.
type implementation struct {
	Name     string
	Document string
	Port     int
}

func (i implementation) Origin() string {
	return fmt.Sprintf("http://127.0.0.1:%d", i.Port)
}

func (i implementation) URL() string {
	return i.Origin() + "/" + i.Document
}

// verifyCorpus insists the corpus holds exactly the 48 example directories this
// population was drawn from. A sweep against a different checkout would still
// run, and its 43 arms would still be labelled, which is why the check is here
// and not left to whoever reads the results.
func verifyCorpus(corpusRoot string) error {
	entries, err := os.ReadDir(filepath.Join(corpusRoot, examplesDirectory))
	if err != nil {
		return fmt.Errorf("--corpus: %w", err)
	}
	var found []string
	for _, entry := range entries {
		if entry.IsDir() {
			found = append(found, entry.Name())
		}
	}
	expected := slices.Concat(includedImplementations, excludedImplementations)
	slices.Sort(expected)
	slices.Sort(found)
	if slices.Equal(expected, found) {
		return nil
	}
	var missing, unexpected []string
	for _, name := range expected {
		if !slices.Contains(found, name) {
			missing = append(missing, name)
		}
	}
	for _, name := range found {
		if !slices.Contains(expected, name) {
			unexpected = append(unexpected, name)
		}
	}
	return fmt.Errorf(
		"%s/%s is not the corpus this population was drawn from: missing %v, unexpected %v",
		corpusRoot,
		examplesDirectory,
		missing,
		unexpected,
	)
}

// selectImplementations resolves --implementations against the population. An
// empty selection is the whole population, which is what a real sweep runs; a
// named subset is for smoke runs and is recorded in the manifest like any other
// intent.
func selectImplementations(selection string) ([]string, error) {
	if strings.TrimSpace(selection) == "" {
		return slices.Clone(includedImplementations), nil
	}
	var names []string
	seen := map[string]bool{}
	for _, part := range strings.Split(selection, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("empty implementation in %q", selection)
		}
		if !slices.Contains(includedImplementations, name) {
			return nil, fmt.Errorf(
				"%q is not one of the %d implementations in this population",
				name,
				len(includedImplementations),
			)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate implementation %q", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

// planImplementations gives every selected implementation its document and its
// own port, in population order so the manifest can name the URL each arm was
// served from before anything has been served.
func planImplementations(
	corpusRoot string,
	names []string,
	basePort int,
) ([]implementation, error) {
	if basePort+len(names)-1 > 65535 {
		return nil, fmt.Errorf(
			"--base-port %d leaves no room for %d implementations",
			basePort,
			len(names),
		)
	}
	planned := make([]implementation, 0, len(names))
	for index, name := range names {
		document := documentFor(name)
		if _, err := os.Stat(filepath.Join(corpusRoot, filepath.FromSlash(document))); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		planned = append(planned, implementation{
			Name:     name,
			Document: document,
			Port:     basePort + index,
		})
	}
	return planned, nil
}
