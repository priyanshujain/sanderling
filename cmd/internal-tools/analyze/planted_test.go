package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// A pipeline exercised only on data whose answer nobody knows reports that it
// runs, not that it is right. These tests plant effects whose value is known in
// advance from the model that generated the data, and require the pipeline to
// recover them from campaign directories it reads off disk through its own
// entry point.
//
// plantedModel is the generator: a run that has not yet violated does so at
// every step with probability Hazard, and a run reaching Budget without
// violating is right-censored there. Steps to first violation is therefore
// geometric, truncated at the budget, and every quantity the pipeline reports
// about it has a closed form below.
type plantedModel struct {
	Hazard float64
	Budget int
}

// recordedValues is the distribution of what one run contributes to the
// analysis: the violation step when it violates, and the budget when it does
// not, since a censored run is held at the budget. Index t carries P(value = t).
func (model plantedModel) recordedValues() []float64 {
	probabilities := make([]float64, model.Budget+1)
	survival := 1.0
	for step := 1; step < model.Budget; step++ {
		probabilities[step] = survival * model.Hazard
		survival *= 1 - model.Hazard
	}
	probabilities[model.Budget] = survival
	return probabilities
}

func (model plantedModel) survivalAt(step int) float64 {
	return math.Pow(1-model.Hazard, float64(step))
}

func (model plantedModel) censoredShare() float64 {
	return model.survivalAt(model.Budget)
}

// medianSteps is the smallest step at which the true survival function falls to
// or below one half, which is what the Kaplan-Meier median estimates.
func (model plantedModel) medianSteps() (int, bool) {
	for step := 1; step <= model.Budget; step++ {
		if model.survivalAt(step) <= 0.5 {
			return step, true
		}
	}
	return 0, false
}

// populationA12 is the Vargha-Delaney effect size between two planted models,
// computed from the models rather than from any sample: the probability that a
// run of the first takes more steps than a run of the second, counting a tie as
// half. Every run held at the budget ties with every other, which is the term a
// pipeline mishandling censoring gets wrong.
func populationA12(first, second plantedModel) float64 {
	left, right := first.recordedValues(), second.recordedValues()
	total := 0.0
	for value, probability := range left {
		if probability == 0 {
			continue
		}
		for other, otherProbability := range right {
			switch {
			case value > other:
				total += probability * otherProbability
			case value == other:
				total += 0.5 * probability * otherProbability
			}
		}
	}
	return total
}

type plantedRun struct {
	seed     int64
	steps    int
	violated bool
}

func (model plantedModel) draw(seed int64, source *rand.Rand) plantedRun {
	for step := 1; step <= model.Budget; step++ {
		if source.Float64() < model.Hazard {
			return plantedRun{seed: seed, steps: step, violated: true}
		}
	}
	return plantedRun{seed: seed, steps: model.Budget}
}

func (model plantedModel) drawCampaign(source *rand.Rand, seeds int) []plantedRun {
	runs := make([]plantedRun, 0, seeds)
	for seed := int64(1); seed <= int64(seeds); seed++ {
		runs = append(runs, model.draw(seed, source))
	}
	return runs
}

// writePlantedCampaign emits the campaign directory shape the campaign tool
// writes, so the planted data reaches the statistics through the same loader,
// classifier and censoring rules as a real sweep.
func writePlantedCampaign(t *testing.T, directory, armName string, budget int, runs []plantedRun) string {
	t.Helper()
	seeds := make([]int64, 0, len(runs))
	records := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		seeds = append(seeds, run.seed)
		record := map[string]any{
			"seed":             run.seed,
			"exit_code":        0,
			"steps":            run.steps,
			"actions":          run.steps,
			"monotonic_millis": int64(run.steps) * 900,
		}
		if run.violated {
			record["first_violation_origin_step"] = run.steps
			record["violated_properties"] = []string{"plantedProperty"}
		} else {
			record["first_violation_origin_step"] = nil
		}
		records = append(records, record)
	}
	writeCampaign(t, directory, map[string]any{
		"arm":       armName,
		"generator": "seeded",
		"platform":  "android",
		"max_steps": budget,
		"seeds":     seeds,
		"host":      "planted",
	}, records)
	return directory
}

// analyseCampaigns runs the tool exactly as the command line does and returns
// the machine-readable summary it wrote.
func analyseCampaigns(t *testing.T, arguments ...string) analysis {
	t.Helper()
	summaryPath := filepath.Join(t.TempDir(), "analysis.json")
	if err := run(append([]string{"--json", summaryPath}, arguments...), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var result analysis
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func armByName(t *testing.T, result analysis, name string) armSummary {
	t.Helper()
	for _, summary := range result.Arms {
		if summary.Arm == name {
			return summary
		}
	}
	t.Fatalf("no arm %q in %v", name, result.Arms)
	return armSummary{}
}

// plantTwoArms writes two campaign directories drawn from the given models and
// returns them in the order they were written.
func plantTwoArms(t *testing.T, sourceSeed int64, seeds int, first, second plantedModel) (string, string) {
	t.Helper()
	root := t.TempDir()
	source := rand.New(rand.NewSource(sourceSeed))
	firstDirectory := writePlantedCampaign(t, filepath.Join(root, "first"), "first", first.Budget,
		first.drawCampaign(source, seeds))
	secondDirectory := writePlantedCampaign(t, filepath.Join(root, "second"), "second", second.Budget,
		second.drawCampaign(source, seeds))
	return firstDirectory, secondDirectory
}

// The effect size is the number the paper reports as the size of a difference,
// so it is checked against the value the generating models define rather than
// against anything this tool produced. Four independent draws are used because
// a tolerance that holds for one lucky sample is not a check.
func TestPlanted_RecoversTheKnownEffectSize(t *testing.T) {
	slow := plantedModel{Hazard: 0.005, Budget: 400}
	fast := plantedModel{Hazard: 0.02, Budget: 400}
	expected := populationA12(slow, fast)
	if expected < 0.6 {
		t.Fatalf("planted effect %v is too small to be worth checking", expected)
	}

	for _, sourceSeed := range []int64{1, 2, 3, 4} {
		slowDirectory, fastDirectory := plantTwoArms(t, sourceSeed, 400, slow, fast)
		result := analyseCampaigns(t, slowDirectory, fastDirectory)

		if len(result.Pairwise) != 1 {
			t.Fatalf("%d comparisons, want 1", len(result.Pairwise))
		}
		pair := result.Pairwise[0]
		if pair.First != "first" || pair.Second != "second" {
			t.Fatalf("comparison %s vs %s, want first vs second", pair.First, pair.Second)
		}
		if math.Abs(pair.A12-expected) > 0.03 {
			t.Errorf("seed %d: a12 %.4f, want the planted %.4f", sourceSeed, pair.A12, expected)
		}
		if pair.PValue > 1e-6 {
			t.Errorf("seed %d: p-value %v for an effect this large", sourceSeed, pair.PValue)
		}
	}
}

// A12 above one half has to mean the first arm took longer, and a sign flip
// anywhere in the pipeline is the failure that produces a plausible wrong
// answer rather than an obvious one.
func TestPlanted_EffectSizeDirectionFollowsTheArmOrder(t *testing.T) {
	slow := plantedModel{Hazard: 0.005, Budget: 400}
	fast := plantedModel{Hazard: 0.02, Budget: 400}
	source := rand.New(rand.NewSource(7))
	slowRuns := slow.drawCampaign(source, 300)
	fastRuns := fast.drawCampaign(source, 300)

	// Comparisons are ordered by arm label, so the same two samples are
	// analysed twice with the labels swapped.
	root := t.TempDir()
	forward := analyseCampaigns(t,
		writePlantedCampaign(t, filepath.Join(root, "slow-first"), "a", slow.Budget, slowRuns),
		writePlantedCampaign(t, filepath.Join(root, "fast-second"), "b", fast.Budget, fastRuns)).Pairwise[0]
	reversed := analyseCampaigns(t,
		writePlantedCampaign(t, filepath.Join(root, "slow-second"), "b", slow.Budget, slowRuns),
		writePlantedCampaign(t, filepath.Join(root, "fast-first"), "a", fast.Budget, fastRuns)).Pairwise[0]
	if forward.A12 <= 0.5 {
		t.Errorf("a12 %.4f with the slower arm first, want above 0.5", forward.A12)
	}
	if math.Abs(forward.A12+reversed.A12-1) > 1e-12 {
		t.Errorf("a12 %.4f and %.4f, want them to sum to 1", forward.A12, reversed.A12)
	}
	if math.Abs(forward.PValue-reversed.PValue) > 1e-12 {
		t.Errorf("p-values %v and %v, want the same two-sided value", forward.PValue, reversed.PValue)
	}
}

// The median and the quartiles are read off the Kaplan-Meier curve, and the
// curve itself is checked point by point against the survival function that
// generated the data. A pipeline counting a censored run as a violation drives
// this estimate down hard.
func TestPlanted_SurvivalCurveTracksTheGeneratingHazard(t *testing.T) {
	model := plantedModel{Hazard: 0.005, Budget: 400}
	if model.censoredShare() < 0.1 {
		t.Fatalf("planted censoring is %.3f, too little to exercise the estimator", model.censoredShare())
	}
	directory, _ := plantTwoArms(t, 11, 600, model, model)
	summary := armByName(t, analyseCampaigns(t, directory), "first")

	for _, step := range []int{20, 50, 100, 200, 300} {
		estimated, ok := survivalAtStep(summary.SurvivalCurve, float64(step))
		if !ok {
			t.Fatalf("curve has no point at or before step %d", step)
		}
		if want := model.survivalAt(step); math.Abs(estimated-want) > 0.05 {
			t.Errorf("survival at %d is %.4f, want the planted %.4f", step, estimated, want)
		}
	}

	median, ok := model.medianSteps()
	if !ok {
		t.Fatal("the planted model has no median to recover")
	}
	if summary.MedianStepsToFirstViolation == nil {
		t.Fatal("median undefined, want the planted median")
	}
	if math.Abs(*summary.MedianStepsToFirstViolation-float64(median)) > 10 {
		t.Errorf("median %v, want the planted %d", *summary.MedianStepsToFirstViolation, median)
	}
	assertQuartile(t, summary.FirstQuartileSteps, model, 0.25)
	assertQuartile(t, summary.ThirdQuartileSteps, model, 0.75)
}

func assertQuartile(t *testing.T, got *float64, model plantedModel, fraction float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("quantile %v undefined, want a value", fraction)
	}
	if want := model.survivalAt(int(*got)); want > 1-fraction+0.05 {
		t.Errorf("quantile %v at step %v, where the planted survival is %.4f", fraction, *got, want)
	}
}

func survivalAtStep(curve []survivalPoint, step float64) (float64, bool) {
	value, found := 0.0, false
	for _, point := range curve {
		if point.Steps > step {
			break
		}
		value, found = point.Survival, true
	}
	return value, found
}

// A true null must not be called significant more often than the test's own
// level allows. This is the check that catches a wrong variance term, which
// leaves every point estimate looking reasonable and only shows up in how often
// the pipeline claims a difference that is not there.
func TestPlanted_TrueNullIsNotCalledSignificantAboveItsLevel(t *testing.T) {
	const replicates = 300
	model := plantedModel{Hazard: 0.01, Budget: 400}
	rejectedByGehan, rejectedByLogRank := 0, 0
	for replicate := 0; replicate < replicates; replicate++ {
		first, second := plantTwoArms(t, int64(1000+replicate), 30, model, model)
		result := analyseCampaigns(t, first, second)
		if result.Pairwise[0].PValue < 0.05 {
			rejectedByGehan++
		}
		if result.LogRank.PValue < 0.05 {
			rejectedByLogRank++
		}
	}
	// Three standard errors around 0.05 at 300 replicates is 0.05 +/- 0.038.
	// The lower bound is asserted too: a test that never rejects has bought its
	// level by losing the power the experiment is sized for.
	for name, rejected := range map[string]int{"gehan": rejectedByGehan, "log-rank": rejectedByLogRank} {
		rate := float64(rejected) / replicates
		if rate > 0.09 || rate < 0.015 {
			t.Errorf("%s called a true null significant in %.1f%% of %d replicates, want about 5%%",
				name, 100*rate, replicates)
		}
	}
}

// The same null with most runs censored, where nearly every observation is tied
// at the budget and the tie correction is what keeps the level honest.
func TestPlanted_TrueNullUnderHeavyCensoringKeepsItsLevel(t *testing.T) {
	const replicates = 300
	model := plantedModel{Hazard: 0.0004, Budget: 400}
	if model.censoredShare() < 0.8 {
		t.Fatalf("planted censoring is %.3f, want most runs censored", model.censoredShare())
	}
	rejected := 0
	for replicate := 0; replicate < replicates; replicate++ {
		first, second := plantTwoArms(t, int64(9000+replicate), 40, model, model)
		if analyseCampaigns(t, first, second).Pairwise[0].PValue < 0.05 {
			rejected++
		}
	}
	if rate := float64(rejected) / replicates; rate > 0.09 {
		t.Errorf("gehan called a true null significant in %.1f%% of %d replicates under heavy censoring, want at most about 5%%",
			100*rate, replicates)
	}
}

// Most runs never violating is the expected shape of an arm at a budget chosen
// for a harder subject, and it is the case where an implementation that drops
// censored runs instead of holding them at the budget still produces a number.
func TestPlanted_HeavyCensoringLeavesTheMedianUndefinedAndKeepsTheEffect(t *testing.T) {
	quiet := plantedModel{Hazard: 0.00025, Budget: 400}
	loud := plantedModel{Hazard: 0.002, Budget: 400}
	if quiet.censoredShare() < 0.85 {
		t.Fatalf("planted censoring is %.3f, want most runs censored", quiet.censoredShare())
	}
	if _, ok := quiet.medianSteps(); ok {
		t.Fatal("the heavily censored model has a median, so the test is not testing what it says")
	}

	quietDirectory, loudDirectory := plantTwoArms(t, 21, 400, quiet, loud)
	result := analyseCampaigns(t, quietDirectory, loudDirectory)
	summary := armByName(t, result, "first")

	if summary.MedianStepsToFirstViolation != nil {
		t.Errorf("median %v, want undefined where fewer than half the runs violate",
			*summary.MedianStepsToFirstViolation)
	}
	if summary.ThirdQuartileSteps != nil {
		t.Errorf("third quartile %v, want undefined", *summary.ThirdQuartileSteps)
	}
	if summary.Usable != 400 {
		t.Errorf("%d usable runs, want all 400: a censored run is data", summary.Usable)
	}
	censoredShare := float64(summary.Censored) / float64(summary.Usable)
	if math.Abs(censoredShare-quiet.censoredShare()) > 0.04 {
		t.Errorf("censored share %.3f, want the planted %.3f", censoredShare, quiet.censoredShare())
	}

	expected := populationA12(quiet, loud)
	pair := result.Pairwise[0]
	if math.Abs(pair.A12-expected) > 0.03 {
		t.Errorf("a12 %.4f under heavy censoring, want the planted %.4f", pair.A12, expected)
	}
	if pair.PValue > 0.01 {
		t.Errorf("p-value %v, want the effect to survive the censoring", pair.PValue)
	}
	if result.LogRank.Observed[0] >= result.LogRank.Expected[0] {
		t.Errorf("log-rank observed %v against expected %v for the quiet arm, want fewer than expected",
			result.LogRank.Observed[0], result.LogRank.Expected[0])
	}
}

// An arm at a real budget is mostly one enormous group of runs censored
// together at it. No exact null distribution covers that, so the conditional
// variance carries the p-value, and it is checked against a permutation p-value
// over the pipeline's own two samples.
func TestPlanted_CensoringAtTheBudgetTracksThePermutationPValue(t *testing.T) {
	cases := []struct {
		sourceSeed  int64
		quiet, loud float64
		seeds       int
	}{
		{33, 0.0008, 0.0025, 60},
		{51, 0.0008, 0.0014, 50},
		{52, 0.0006, 0.0012, 50},
		{53, 0.0010, 0.0016, 50},
	}
	discriminating := 0
	for _, test := range cases {
		quiet := plantedModel{Hazard: test.quiet, Budget: 400}
		loud := plantedModel{Hazard: test.loud, Budget: 400}
		source := rand.New(rand.NewSource(test.sourceSeed))
		quietRuns := quiet.drawCampaign(source, test.seeds)
		loudRuns := loud.drawCampaign(source, test.seeds)

		root := t.TempDir()
		quietDirectory := writePlantedCampaign(t, filepath.Join(root, "quiet"), "quiet", quiet.Budget, quietRuns)
		loudDirectory := writePlantedCampaign(t, filepath.Join(root, "loud"), "loud", loud.Budget, loudRuns)
		pair := analyseCampaigns(t, loudDirectory, quietDirectory).Pairwise[0]

		permuted := permutationGehanTwoSided(plantedObservations(loudRuns), plantedObservations(quietRuns))
		if permuted > 0.01 {
			discriminating++
		}
		// The conditional variance and the permutation variance are two
		// estimators and not one, so they are near rather than equal. Over these
		// four samples the gap is at most 0.011, at a p-value of 0.49 where
		// nothing is decided; where a decision is made it is under 0.005. A
		// variance wrong by a factor moves the p-value across orders of
		// magnitude, which is what this catches.
		if math.Abs(pair.PValue-permuted) > 0.015 {
			t.Errorf("hazards %v against %v: p-value %.4f, want near the permutation p-value %.4f",
				test.quiet, test.loud, pair.PValue, permuted)
		}
	}
	// A permutation p-value already at the floor cannot show a variance moving
	// by a fraction, so the check has to include cases that are not overwhelming.
	if discriminating < 3 {
		t.Fatalf("%d of %d cases carry a p-value large enough to discriminate", discriminating, len(cases))
	}
}

func plantedObservations(runs []plantedRun) []observation {
	items := make([]observation, 0, len(runs))
	for _, run := range runs {
		items = append(items, observation{Steps: float64(run.steps), Event: run.violated})
	}
	return items
}

// permutationGehanTwoSided is the randomization p-value of Gehan's statistic:
// relabel the pooled runs many times, censoring and all, and count how often the
// statistic lands at least as far from zero as the observed one. Both arms here
// censor on the same schedule, which is what makes relabelling them a null.
func permutationGehanTwoSided(first, second []observation) float64 {
	pooled := append(append([]observation{}, first...), second...)
	observed, _ := gehanReference(first, second)
	source := rand.New(rand.NewSource(99))
	const shuffles = 20000
	extreme := 0
	for shuffle := 0; shuffle < shuffles; shuffle++ {
		source.Shuffle(len(pooled), func(left, right int) {
			pooled[left], pooled[right] = pooled[right], pooled[left]
		})
		statistic, _ := gehanReference(pooled[:len(first)], pooled[len(first):])
		if math.Abs(statistic) >= math.Abs(observed)-1e-9 {
			extreme++
		}
	}
	return float64(extreme) / shuffles
}

// Holm has to change an answer somewhere, or its presence in the pipeline is
// decoration. Six arms drawn from hazards close together produce a family where
// uncorrected p-values call several comparisons significant and the correction
// withdraws some of them.
func TestPlanted_HolmWithdrawsAConclusionUncorrectedPValuesReach(t *testing.T) {
	budget := 400
	hazards := []float64{0.0100, 0.0125, 0.0150, 0.0175, 0.0200, 0.0225}
	root := t.TempDir()
	source := rand.New(rand.NewSource(4242))
	var directories []string
	for index, hazard := range hazards {
		model := plantedModel{Hazard: hazard, Budget: budget}
		name := fmt.Sprintf("arm%d", index)
		directories = append(directories, writePlantedCampaign(t,
			filepath.Join(root, name), name, budget, model.drawCampaign(source, 40)))
	}

	result := analyseCampaigns(t, directories...)
	if len(result.Pairwise) != 15 {
		t.Fatalf("%d comparisons, want 15 over six arms", len(result.Pairwise))
	}
	if result.HolmFamilySize != 15 {
		t.Errorf("holm family size %d, want 15", result.HolmFamilySize)
	}

	uncorrected, corrected, withdrawn := 0, 0, 0
	for _, pair := range result.Pairwise {
		if pair.PValue < 0.05 {
			uncorrected++
		}
		if pair.HolmPValue < 0.05 {
			corrected++
		}
		if pair.PValue < 0.05 && pair.HolmPValue >= 0.05 {
			withdrawn++
		}
	}
	if withdrawn == 0 {
		t.Fatalf("holm withdrew no conclusion: %d of 15 comparisons significant before and after", uncorrected)
	}
	if corrected >= uncorrected {
		t.Errorf("%d comparisons significant uncorrected and %d after holm, want fewer", uncorrected, corrected)
	}
}

// Holm is applied within one research question and not across the paper, so the
// same two campaigns analysed on their own must keep their raw p-value while the
// same comparison inside a larger family is corrected against that family.
func TestPlanted_HolmFamilyIsTheInvocationAndNotEveryComparisonEverMade(t *testing.T) {
	budget := 400
	root := t.TempDir()
	source := rand.New(rand.NewSource(777))
	var directories []string
	for index, hazard := range []float64{0.010, 0.014, 0.018, 0.022} {
		model := plantedModel{Hazard: hazard, Budget: budget}
		name := fmt.Sprintf("arm%d", index)
		directories = append(directories, writePlantedCampaign(t,
			filepath.Join(root, name), name, budget, model.drawCampaign(source, 40)))
	}

	whole := analyseCampaigns(t, append([]string{"--question", "RQ4"}, directories...)...)
	alone := analyseCampaigns(t, "--question", "RQ4a", directories[0], directories[1])

	if whole.Question != "RQ4" || alone.Question != "RQ4a" {
		t.Errorf("questions %q and %q, want them recorded next to the corrected values", whole.Question, alone.Question)
	}
	if whole.HolmFamilySize != 6 || alone.HolmFamilySize != 1 {
		t.Fatalf("family sizes %d and %d, want 6 and 1", whole.HolmFamilySize, alone.HolmFamilySize)
	}
	if alone.Pairwise[0].HolmPValue != alone.Pairwise[0].PValue {
		t.Errorf("holm p %v against raw p %v in a family of one, want them equal",
			alone.Pairwise[0].HolmPValue, alone.Pairwise[0].PValue)
	}

	var inFamily pairwiseResult
	for _, pair := range whole.Pairwise {
		if pair.First == alone.Pairwise[0].First && pair.Second == alone.Pairwise[0].Second {
			inFamily = pair
		}
	}
	if math.Abs(inFamily.PValue-alone.Pairwise[0].PValue) > 1e-12 {
		t.Fatalf("the same comparison has raw p %v in one family and %v in the other",
			inFamily.PValue, alone.Pairwise[0].PValue)
	}
	if inFamily.HolmPValue <= alone.Pairwise[0].HolmPValue {
		t.Errorf("holm p %v inside a family of six, want it above the %v it carries alone",
			inFamily.HolmPValue, alone.Pairwise[0].HolmPValue)
	}
}

// The ablation's outcome is a per-seed difference with a sign, so the planted
// shift is applied seed by seed and the pipeline has to return that shift with
// that sign from the campaign directories alone.
func TestPlanted_PairedComparisonRecoversTheShiftAndItsSign(t *testing.T) {
	budget := 400
	base := plantedModel{Hazard: 0.01, Budget: budget}
	const shift = 60
	source := rand.New(rand.NewSource(5150))

	var post, pre []plantedRun
	// Arms are contrasted in the order their labels sort, so the plant is stated
	// the same way: post-repair against pre-repair. A seed where the unshifted
	// run violated is a pair the shifted run is known to have outlived, whether
	// it violated later or ran on clean; a seed where neither violated is a pair
	// with no order.
	firstSooner, bothViolated, unordered := 0, 0, 0
	for seed := int64(1); seed <= 30; seed++ {
		fast := base.draw(seed, source)
		slow := plantedRun{seed: seed, steps: fast.steps + shift, violated: fast.violated}
		if !fast.violated || slow.steps > budget {
			slow = plantedRun{seed: seed, steps: budget}
		}
		post = append(post, fast)
		pre = append(pre, slow)
		switch {
		case !fast.violated:
			unordered++
		case slow.violated:
			bothViolated++
			firstSooner++
		default:
			firstSooner++
		}
	}
	if bothViolated == 0 {
		t.Fatal("no pair has two violations, so the planted shift is nowhere the analysis can read it")
	}

	root := t.TempDir()
	preDirectory := writePlantedCampaign(t, filepath.Join(root, "pre"), "pre-repair", budget, pre)
	postDirectory := writePlantedCampaign(t, filepath.Join(root, "post"), "post-repair", budget, post)
	result := analyseCampaigns(t, "--paired", "--question", "RQ4 ablation", preDirectory, postDirectory)

	if result.Paired == nil {
		t.Fatal("no paired comparison, want one")
	}
	if len(result.Pairwise) != 0 {
		t.Errorf("%d unpaired comparisons alongside the paired one, want none in a seed-matched design",
			len(result.Pairwise))
	}
	paired := *result.Paired
	if paired.First != "post-repair" || paired.Second != "pre-repair" {
		t.Fatalf("paired %s minus %s, want post-repair minus pre-repair", paired.First, paired.Second)
	}
	if paired.Pairs != 30 {
		t.Errorf("%d pairs, want 30", paired.Pairs)
	}
	// Every pair where both runs violated was shifted by exactly the plant, so
	// the median over them is the plant itself rather than a mixture of it with
	// the step counts censored runs never reached.
	if paired.BothViolated != bothViolated {
		t.Errorf("%d pair(s) with two violations, want %d", paired.BothViolated, bothViolated)
	}
	if paired.MedianDifference == nil || *paired.MedianDifference != -shift {
		t.Errorf("median difference %v, want the planted %d", paired.MedianDifference, -shift)
	}
	if paired.Sign != -1 {
		t.Errorf("sign %+d, want -1 for the arm that violated sooner", paired.Sign)
	}
	if paired.FirstSooner != firstSooner || paired.SecondSooner != 0 || paired.Unordered != unordered {
		t.Errorf("counts %+v, want %d favouring the shifted arm, none the other way and %d unordered",
			paired, firstSooner, unordered)
	}
	if want := 0.5 * float64(unordered) / 30; paired.A12 != want {
		t.Errorf("a12 within pairs %v, want %v where no pair favours the first arm", paired.A12, want)
	}
	if paired.PValue == nil || *paired.PValue > 0.001 {
		t.Errorf("p-value %v for a shift planted in every pair", paired.PValue)
	}
	if *paired.HolmPValue != *paired.PValue {
		t.Errorf("holm p %v in a family of one, want the raw %v", *paired.HolmPValue, *paired.PValue)
	}
}

// The paired test is the ablation's decision rule, so a null there has to stay a
// null: two arms drawn from the same model, matched by seed, must not report a
// difference more often than the level allows.
func TestPlanted_PairedNullIsNotCalledSignificantAboveItsLevel(t *testing.T) {
	model := plantedModel{Hazard: 0.01, Budget: 400}
	const replicates = 200
	rejected := 0
	for replicate := 0; replicate < replicates; replicate++ {
		first, second := plantTwoArms(t, int64(5000+replicate), 30, model, model)
		result := analyseCampaigns(t, "--paired", first, second)
		if *result.Paired.PValue < 0.05 {
			rejected++
		}
	}
	if rate := float64(rejected) / replicates; rate > 0.10 {
		t.Errorf("the paired test called a true null significant in %.1f%% of %d replicates, want about 5%%",
			100*rate, replicates)
	}
}
