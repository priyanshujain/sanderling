// Command label-coverage reports how much of an app's interactive surface a
// spec can address, from the hierarchies a run already recorded.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type element struct {
	ResourceID  string `json:"resourceId"`
	Text        string `json:"text"`
	Description string `json:"description"`
	Class       string `json:"class"`
	Package     string `json:"package"`
	Clickable   bool   `json:"clickable"`
	Editable    bool   `json:"editable"`
	Enabled     bool   `json:"enabled"`
}

type step struct {
	Index     int    `json:"step"`
	Screen    string `json:"screen"`
	Hierarchy *struct {
		Elements []element `json:"elements"`
	} `json:"hierarchy"`
}

// Counts splits a screen's interactive elements by the strongest selector that
// can reach them. Text is separated from identifier and description because a
// row labelled only by the customer name it displays is addressable in one run
// and gone in the next, which is not the same thing as being addressable. A
// description that carries the data with it is separated for the same reason.
type Counts struct {
	Screen        string `json:"screen"`
	Observations  int    `json:"observations"`
	Elements      int    `json:"elements"`
	Interactive   int    `json:"interactive"`
	ByIdentifier  int    `json:"by_identifier"`
	ByDataID      int    `json:"by_data_carrying_identifier"`
	ByDescription int    `json:"by_description"`
	ByVolatile    int    `json:"by_volatile_description"`
	ByTextOnly    int    `json:"by_text_only"`
	Unaddressable int    `json:"unaddressable"`
	AmbiguousIDs  int    `json:"ambiguous_identifiers"`
	// Needing lists the interactive elements no durable selector reaches, which
	// is the work list for a label pass rather than a statistic about it.
	Needing []string `json:"needing_labels,omitempty"`
}

func (c Counts) stableShare() float64 {
	if c.Interactive == 0 {
		return 0
	}
	return float64(
		c.ByIdentifier+c.ByDescription,
	) / float64(
		c.Interactive,
	) * 100
}

func main() {
	jsonOut := flag.Bool("json", false, "emit JSON instead of a table")
	show := flag.Int(
		"show",
		0,
		"list up to this many controls per screen that no durable selector reaches",
	)
	pkg := flag.String(
		"package",
		"",
		"count only elements belonging to this package; system UI is dropped either way",
	)
	flag.Usage = func() {
		fmt.Fprintln(
			os.Stderr,
			"usage: label-coverage [--json] [--show N] <trace.jsonl | run directory> ...",
		)
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	traces, err := collect(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(traces) == 0 {
		fmt.Fprintln(os.Stderr, "no trace.jsonl found under the given paths")
		os.Exit(1)
	}

	byScreen := map[string]*Counts{}
	for _, path := range traces {
		if err := accumulate(path, byScreen, *pkg); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
	}

	screens := make([]Counts, 0, len(byScreen))
	for _, counts := range byScreen {
		screens = append(screens, *counts)
	}
	sort.Slice(
		screens,
		func(i, j int) bool { return screens[i].Screen < screens[j].Screen },
	)

	if *jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(screens); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	render(os.Stdout, screens, len(traces), *show)
}

func collect(paths []string) ([]string, error) {
	var traces []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			traces = append(traces, path)
			continue
		}
		err = filepath.WalkDir(
			path,
			func(candidate string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() && entry.Name() == "trace.jsonl" {
					traces = append(traces, candidate)
				}
				return nil
			},
		)
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(traces)
	return traces, nil
}

// accumulate folds one trace into byScreen, counting each distinct hierarchy
// once. A run that idles on a screen observes it many times, and summing those
// observations would report the screen the explorer sat on rather than the
// screen with the most unlabelled controls.
// systemPackages own nodes that share the screen with the app under test. A
// status bar contributes three well-labelled controls to every capture, and
// counting them lifts an app with no identifiers at all off the floor.
var systemPackages = map[string]bool{
	"com.android.systemui":                    true,
	"android":                                 true,
	"com.google.android.inputmethod.latin":    true,
	"com.android.inputmethod.latin":           true,
	"com.google.android.apps.nexuslauncher":   true,
	"com.google.android.googlequicksearchbox": true,
}

// scoped drops the windows the app under test does not own. Most of an app's
// own nodes carry no package attribute at all, since only the window roots are
// stamped with one, so an empty package is treated as belonging to the app
// rather than filtered out. Filtering on an exact match alone discards the
// entire application and reports a clean zero.
func scoped(elements []element, pkg string) []element {
	kept := make([]element, 0, len(elements))
	for _, item := range elements {
		if item.Package == "" {
			kept = append(kept, item)
			continue
		}
		if pkg != "" {
			if item.Package == pkg {
				kept = append(kept, item)
			}
			continue
		}
		if !systemPackages[item.Package] {
			kept = append(kept, item)
		}
	}
	return kept
}

func accumulate(path string, byScreen map[string]*Counts, pkg string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	seen := map[string]bool{}
	decoder := json.NewDecoder(file)
	for {
		var recorded step
		if err := decoder.Decode(&recorded); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if recorded.Hierarchy == nil || len(recorded.Hierarchy.Elements) == 0 {
			continue
		}
		elements := scoped(recorded.Hierarchy.Elements, pkg)
		if len(elements) == 0 {
			continue
		}
		screen := recorded.Screen
		if screen == "" {
			screen = shape(elements)
		}
		key := screen + "\x00" + signature(elements)
		if seen[key] {
			continue
		}
		seen[key] = true

		observed := measure(elements)
		observed.Screen = screen
		counts, ok := byScreen[screen]
		if !ok {
			observed.Observations = 1
			byScreen[screen] = &observed
			continue
		}
		if observed.Interactive > counts.Interactive {
			observed.Observations = counts.Observations
			*counts = observed
		}
		counts.Observations++
	}
	return nil
}

// shape names a screen the app did not name itself, by the set of distinct
// controls it shows. Repetition is dropped deliberately: a ledger holding three
// rows and the same ledger holding five is one screen, not two.
func shape(elements []element) string {
	distinct := map[string]bool{}
	for _, item := range elements {
		key := item.ResourceID
		if key == "" {
			key = item.Class + "/" + item.Description
		}
		distinct[key] = true
	}
	keys := make([]string, 0, len(distinct))
	for key := range distinct {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\x00")))
	return "shape:" + hex.EncodeToString(sum[:])[:8]
}

// volatile reports whether a description carries the data it labels. Compose
// merges a row's children into one description, so a ledger row arrives
// labelled with the customer name, the balance and a relative age that reprices
// itself every month. Such a description exists, which is why a presence check
// scores it as a selector, and it is not one: the run that recorded it is the
// only run it matches. The test is a heuristic and deliberately blunt, since
// the alternative is to call every one of them stable.
func volatile(description string) bool {
	trimmed := strings.TrimSpace(description)
	// A one-character label is an avatar initial, and it is the first letter of
	// a name the run happened to observe. Renaming the record changes it. The
	// digit test below catches an initial drawn from a numeric name and would
	// miss every alphabetic one, which is how four of them were counted as
	// durable before this was noticed on a real ledger.
	if len([]rune(trimmed)) == 1 &&
		(unicode.IsLetter([]rune(trimmed)[0]) || unicode.IsDigit([]rune(trimmed)[0])) {
		return true
	}
	for _, character := range trimmed {
		if unicode.IsDigit(character) {
			return true
		}
	}
	lowered := strings.ToLower(trimmed)
	for _, marker := range []string{" ago", "since ", "yesterday", "today", "tomorrow", "last ", "due ", "minute", "hour", "day", "week", "month", "year"} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// dataCarryingID reports whether an identifier embeds the record it names. A
// list row tagged `customer_row_<uuid>` names its role durably and its instance
// not at all, so an exact match on it survives exactly one run. The prefix is
// still worth something, which is what `idPrefix:` is for, but a measure that
// counts the whole string as a durable selector overstates what a spec can say.
// Scoped to the local name so an Android package prefix cannot trip it, and
// tuned to leave ordinary names like `button2` alone.
func dataCarryingID(identifier string) bool {
	local := identifier
	if index := strings.LastIndex(local, "/"); index >= 0 {
		local = local[index+1:]
	}
	digits := 0
	for _, character := range local {
		if unicode.IsDigit(character) {
			digits++
			if digits >= 4 {
				return true
			}
			continue
		}
		digits = 0
	}
	hex, groups := 0, 0
	for _, character := range local + "-" {
		if isHexDigit(character) {
			hex++
			continue
		}
		if hex >= 4 {
			groups++
		}
		hex = 0
	}
	return groups >= 2
}

func isHexDigit(character rune) bool {
	return unicode.IsDigit(character) ||
		(character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F')
}

// keypadDigits reports whether this screen shows a numeric keypad, in which case
// its single-character digit labels name fixed keys rather than the first
// character of somebody's name. Without the screen for context the two are
// indistinguishable: an avatar initial and a calculator key are both one
// character, and only one of them survives a data change.
func keypadDigits(elements []element) bool {
	seen := map[rune]bool{}
	for _, item := range elements {
		label := strings.TrimSpace(item.Description)
		if label == "" {
			label = strings.TrimSpace(item.Text)
		}
		runes := []rune(label)
		if len(runes) == 1 && unicode.IsDigit(runes[0]) {
			seen[runes[0]] = true
		}
	}
	return len(seen) >= 6
}

func isSingleDigit(label string) bool {
	runes := []rune(strings.TrimSpace(label))
	return len(runes) == 1 && unicode.IsDigit(runes[0])
}

func measure(elements []element) Counts {
	counts := Counts{Elements: len(elements)}
	keypad := keypadDigits(elements)
	identifiers := map[string]int{}
	for _, item := range elements {
		if !item.Clickable && !item.Editable {
			continue
		}
		counts.Interactive++
		switch {
		case item.ResourceID != "" && !dataCarryingID(item.ResourceID):
			counts.ByIdentifier++
			identifiers[item.ResourceID]++
		case item.ResourceID != "":
			counts.ByDataID++
			counts.Needing = append(counts.Needing, describe(item))
		case item.Description != "" && keypad && isSingleDigit(item.Description):
			counts.ByDescription++
		case item.Description != "" && !volatile(item.Description):
			counts.ByDescription++
		case item.Description != "":
			counts.ByVolatile++
			counts.Needing = append(counts.Needing, describe(item))
		case item.Text != "":
			counts.ByTextOnly++
			counts.Needing = append(counts.Needing, describe(item))
		default:
			counts.Unaddressable++
			counts.Needing = append(counts.Needing, describe(item))
		}
	}
	for _, repeats := range identifiers {
		if repeats > 1 {
			counts.AmbiguousIDs += repeats
		}
	}
	return counts
}

func describe(item element) string {
	class := item.Class
	if class == "" {
		class = "(no class)"
	}
	switch {
	case item.ResourceID != "" && dataCarryingID(item.ResourceID):
		return fmt.Sprintf(
			"%s id=%q (names its record, not its role)",
			class,
			item.ResourceID,
		)
	case item.Description != "":
		return fmt.Sprintf(
			"%s desc=%q (carries its own data)",
			class,
			item.Description,
		)
	case item.Text != "":
		return fmt.Sprintf("%s text=%q", class, item.Text)
	default:
		return class + " (no label at all)"
	}
}

func signature(elements []element) string {
	var builder strings.Builder
	for _, item := range elements {
		builder.WriteString(item.Class)
		builder.WriteByte('|')
		builder.WriteString(item.ResourceID)
		builder.WriteByte('|')
		builder.WriteString(item.Description)
		builder.WriteByte(';')
	}
	return builder.String()
}

func render(out io.Writer, screens []Counts, traces int, show int) {
	fmt.Fprintf(out, "%d trace(s), %d screen(s)\n\n", traces, len(screens))
	fmt.Fprintf(
		out,
		"%-28s %5s %5s %5s %4s %5s %5s %5s %5s %7s\n",
		"screen",
		"obs",
		"inter",
		"id",
		"id~",
		"desc",
		"vol",
		"text",
		"none",
		"stable%",
	)
	total := Counts{Screen: "TOTAL"}
	for _, screen := range screens {
		fmt.Fprintf(
			out,
			"%-28s %5d %5d %5d %4d %5d %5d %5d %5d %6.1f%%\n",
			truncate(
				screen.Screen,
				28,
			),
			screen.Observations,
			screen.Interactive,
			screen.ByIdentifier,
			screen.ByDataID,
			screen.ByDescription,
			screen.ByVolatile,
			screen.ByTextOnly,
			screen.Unaddressable,
			screen.stableShare(),
		)
		total.Observations += screen.Observations
		total.Elements += screen.Elements
		total.Interactive += screen.Interactive
		total.ByIdentifier += screen.ByIdentifier
		total.ByDataID += screen.ByDataID
		total.ByDescription += screen.ByDescription
		total.ByVolatile += screen.ByVolatile
		total.ByTextOnly += screen.ByTextOnly
		total.Unaddressable += screen.Unaddressable
		total.AmbiguousIDs += screen.AmbiguousIDs
	}
	fmt.Fprintf(out, "%-28s %5d %5d %5d %4d %5d %5d %5d %5d %6.1f%%\n",
		total.Screen, total.Observations, total.Interactive, total.ByIdentifier,
		total.ByDataID, total.ByDescription, total.ByVolatile, total.ByTextOnly,
		total.Unaddressable, total.stableShare())
	fmt.Fprintf(
		out,
		"\n%d interactive element(s) carry an identifier, which is the number that survives a data change\n",
		total.ByIdentifier,
	)
	fmt.Fprintf(
		out,
		"%d share a resource id with another on the same screen\n",
		total.AmbiguousIDs,
	)
	if show <= 0 {
		return
	}
	for _, screen := range screens {
		if len(screen.Needing) == 0 {
			continue
		}
		fmt.Fprintf(
			out,
			"\n%s, %d control(s) no durable selector reaches:\n",
			screen.Screen,
			len(screen.Needing),
		)
		for index, item := range screen.Needing {
			if index == show {
				fmt.Fprintf(
					out,
					"  ... and %d more\n",
					len(screen.Needing)-show,
				)
				break
			}
			fmt.Fprintf(out, "  %s\n", item)
		}
	}
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	return value[:width-1] + "~"
}
