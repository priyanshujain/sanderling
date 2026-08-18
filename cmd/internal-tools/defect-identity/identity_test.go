package main

import (
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/tracecorpus"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// TestOneDefectSeenTwiceIsOneInstance: two runs report the same property from
// the same origin action on the same screen, which is one defect found twice
// and two properties violated.
func TestOneDefectSeenTwiceIsOneInstance(t *testing.T) {
	first := run(t, 3,
		step(1, "/ledger", tap("id:add-txn")),
		violating(2, "/accounts/7", tap("id:save"), "balanceMatches", 2, 2),
	)
	second := run(t, 5,
		step(1, "/ledger", tap("id:add-txn")),
		violating(2, "/accounts/7", tap("id:save"), "balanceMatches", 2, 2),
	)

	corpus := identified(t, bySelector, first, second)
	if len(corpus.Instances) != 1 {
		t.Fatalf("instances = %d, want 1: %+v", len(corpus.Instances), corpus.Instances)
	}
	instance := corpus.Instances[0]
	if len(instance.Runs) != 2 || instance.Reports != 2 {
		t.Fatalf("instance = %+v, want two runs reporting it", instance)
	}
	if corpus.Singletons() != 0 {
		t.Fatalf("singletons = %d, want 0", corpus.Singletons())
	}
}

func TestTheSamePropertyFromTwoOriginsIsTwoDefects(t *testing.T) {
	first := run(t, 3, violating(1, "/ledger", tap("id:save"), "balanceMatches", 1, 1))
	second := run(t, 5, violating(1, "/ledger", tap("id:delete"), "balanceMatches", 1, 1))

	corpus := identified(t, bySelector, first, second)
	if len(corpus.Instances) != 2 {
		t.Fatalf("instances = %d, want 2: %+v", len(corpus.Instances), corpus.Instances)
	}
	if corpus.Singletons() != 2 {
		t.Fatalf("singletons = %d, want 2", corpus.Singletons())
	}
}

func TestTheSamePropertyOnTwoScreensIsTwoDefects(t *testing.T) {
	first := run(t, 3, violating(1, "/ledger", tap("id:save"), "balanceMatches", 1, 1))
	second := run(t, 5, violating(1, "/home", tap("id:save"), "balanceMatches", 1, 1))

	corpus := identified(t, bySelector, first, second)
	if len(corpus.Instances) != 2 {
		t.Fatalf("instances = %d, want 2: %+v", len(corpus.Instances), corpus.Instances)
	}
}

// TestADeferredViolationTakesTheActionOnItsOriginLine holds the alignment the
// draft states: the action recorded on line k is the one applied after
// observing k, so an obligation armed at k is attributed to that action and
// witnessed on the screen the detection step observed.
func TestADeferredViolationTakesTheActionOnItsOriginLine(t *testing.T) {
	only := run(t, 3,
		step(1, "/login", tap("id:login-submit")),
		step(2, "/home", tap("id:open-ledger")),
		violating(3, "/ledger", tap("id:add-txn"), "landsOnLedger", 2, 3),
	)

	corpus := identified(t, bySelector, only)
	instance := corpus.Instances[0]
	if instance.OriginAction != "Tap id:open-ledger" {
		t.Fatalf("origin action = %q, want the action on the origin line", instance.OriginAction)
	}
	if instance.WitnessScreen != "/ledger" {
		t.Fatalf("witness screen = %q, want the detection step's screen", instance.WitnessScreen)
	}
}

func TestAViolationWithNoWitnessIsReportedNotCounted(t *testing.T) {
	unattributed := trace.Step{
		Index:      1,
		Screen:     "/ledger",
		Violations: []string{"balanceMatches"},
	}
	corpus := identified(t, bySelector, run(t, 3, unattributed))

	if len(corpus.Instances) != 0 {
		t.Fatalf("instances = %d, want 0: %+v", len(corpus.Instances), corpus.Instances)
	}
	if len(corpus.Unattributed) != 1 ||
		corpus.Unattributed[0].Property != "balanceMatches" {
		t.Fatalf("unattributed = %+v, want the one violation with no origin", corpus.Unattributed)
	}
}

// TestTheStrictActionKeySplitsWhatTheSelectorKeyMerges quantifies the reading
// the draft leaves open: two runs that typed different text into the same
// field are one defect by selector and two by the whole action.
func TestTheStrictActionKeySplitsWhatTheSelectorKeyMerges(t *testing.T) {
	first := run(t, 3, violating(1, "/ledger", typing("id:amount", "12"), "balanceMatches", 1, 1))
	second := run(t, 5, violating(1, "/ledger", typing("id:amount", "9000"), "balanceMatches", 1, 1))

	if got := identified(t, bySelector, first, second); len(got.Instances) != 1 {
		t.Fatalf("by selector: instances = %d, want 1", len(got.Instances))
	}
	if got := identified(t, byFullAction, first, second); len(got.Instances) != 2 {
		t.Fatalf("by full action: instances = %d, want 2", len(got.Instances))
	}
}

// TestARedactedTypedValueDegradesTheFullKeyVisibly: two runs typed different
// values into one field, both reached the trace redacted, and the whole action
// can no longer tell them apart. The pair is one row, and the report has to say
// so rather than let it read as one defect found twice.
func TestARedactedTypedValueDegradesTheFullKeyVisibly(t *testing.T) {
	first := run(t, 3, violating(1, "/login",
		typing("id:password", recordedText(t, "hunter2")), "staysSignedIn", 1, 1))
	second := run(t, 5, violating(1, "/login",
		typing("id:password", recordedText(t, "correct horse")), "staysSignedIn", 1, 1))

	corpus := identified(t, byFullAction, first, second)
	if len(corpus.Instances) != 1 {
		t.Fatalf("instances = %d, want the redacted pair to be one row: %+v",
			len(corpus.Instances), corpus.Instances)
	}
	report := rendered(corpus)
	if !strings.Contains(report, "1 identity") || !strings.Contains(report, "redacted") {
		t.Fatalf("report does not say one identity rests on a redacted value:\n%s", report)
	}
}

func TestARedactedOriginKeepsTheSelectorApart(t *testing.T) {
	first := run(t, 3, violating(1, "/login",
		typing("id:password", recordedText(t, "hunter2")), "staysSignedIn", 1, 1))
	second := run(t, 5, violating(1, "/login",
		typing("id:pin", recordedText(t, "hunter2")), "staysSignedIn", 1, 1))

	corpus := identified(t, byFullAction, first, second)
	if len(corpus.Instances) != 2 {
		t.Fatalf("instances = %d, want two fields to stay two rows: %+v",
			len(corpus.Instances), corpus.Instances)
	}
	if report := rendered(corpus); !strings.Contains(report, "2 identity") {
		t.Fatalf("report does not count both degraded identities:\n%s", report)
	}
}

func TestARedactedOriginDegradesNothingUnderTheSelectorKey(t *testing.T) {
	only := run(t, 3, violating(1, "/login",
		typing("id:password", recordedText(t, "hunter2")), "staysSignedIn", 1, 1))

	if report := rendered(identified(t, bySelector, only)); strings.Contains(report, "redacted") {
		t.Fatalf("selector key reads no text, so nothing degrades:\n%s", report)
	}
}

// recordedText renders a typed value the way the runner records it, so what the
// key sees is redaction as it really happens and not a placeholder the test
// wrote itself.
func recordedText(t *testing.T, typed string) string {
	t.Helper()
	recorded := verifier.RecordedActionText(verifier.Action{
		Kind: verifier.ActionKindInputText,
		On:   "id:password",
		Text: typed,
	}, nil)
	if recorded == typed {
		t.Fatalf("typed value %q reached the record unredacted", typed)
	}
	return recorded
}

func rendered(corpus Corpus) string {
	var report strings.Builder
	render(&report, corpus)
	return report.String()
}

func identified(t *testing.T, mode actionKeyMode, runs ...tracecorpus.Run) Corpus {
	t.Helper()
	corpus, err := identify(runs, mode)
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

func run(t *testing.T, seed int64, steps ...trace.Step) tracecorpus.Run {
	t.Helper()
	directory := t.TempDir()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteMeta(trace.Meta{Seed: seed, Platform: "web"}); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if err := writer.WriteStep(step); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := tracecorpus.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func step(index int, screen string, action *trace.Action) trace.Step {
	return trace.Step{Index: index, Screen: screen, NextAction: action}
}

func violating(
	index int,
	screen string,
	action *trace.Action,
	property string,
	origin int,
	detected int,
) trace.Step {
	violated := step(index, screen, action)
	violated.Violations = []string{property}
	violated.Witnesses = map[string]trace.Witness{
		property: {Reason: "predicate false", Step: origin, DetectedStep: detected},
	}
	return violated
}

func tap(selector string) *trace.Action {
	return &trace.Action{Kind: "Tap", Selector: selector, X: 10, Y: 20}
}

func typing(selector string, text string) *trace.Action {
	return &trace.Action{Kind: "InputText", Selector: selector, Text: text, X: 10, Y: 20}
}
