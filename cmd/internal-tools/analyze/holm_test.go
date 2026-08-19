package main

import (
	"math"
	"testing"
)

// Both cases are printed R output in Eve Slavich, "Four strategies for dealing
// with multiple comparisons", UNSW Stats Central, slides 9 and 10:
//
//	pValues = c(0.01, 0.2, 0.08, 0.03)
//	p.adjust(pValues, method = "holm")
//	## [1] 0.04 0.20 0.16 0.09
//
//	pValues = c(0.01, 0.2, 0.08, 0.03, 0.02, 0.01)
//	p.adjust(pValues, method = "holm")
//	## [1] 0.06 0.20 0.16 0.09 0.08 0.06
//
// The second case exercises the monotonicity step: sorted p are
// .01 .01 .02 .03 .08 .20, scaled by 6 5 4 3 2 1 to .06 .05 .08 .09 .16 .20,
// and the running maximum lifts the second back to .06.
func TestHolm_MatchesPublishedAdjustment(t *testing.T) {
	cases := []struct {
		raw      []float64
		expected []float64
	}{
		{[]float64{0.01, 0.2, 0.08, 0.03}, []float64{0.04, 0.20, 0.16, 0.09}},
		{[]float64{0.01, 0.2, 0.08, 0.03, 0.02, 0.01}, []float64{0.06, 0.20, 0.16, 0.09, 0.08, 0.06}},
	}
	for _, test := range cases {
		adjusted := holm(test.raw)
		for index, want := range test.expected {
			if math.Abs(adjusted[index]-want) > 1e-12 {
				t.Errorf("holm(%v)[%d] = %v, want %v", test.raw, index, adjusted[index], want)
			}
		}
	}
}

func TestHolm_CapsAtOneAndKeepsOrder(t *testing.T) {
	adjusted := holm([]float64{0.4, 0.5, 0.9})
	for index, value := range adjusted {
		if value != 1 {
			t.Errorf("adjusted[%d] = %v, want 1", index, value)
		}
	}
	single := holm([]float64{0.03})
	if len(single) != 1 || single[0] != 0.03 {
		t.Errorf("single comparison adjusted to %v, want 0.03 unchanged", single)
	}
	if got := holm(nil); len(got) != 0 {
		t.Errorf("holm(nil) = %v, want empty", got)
	}
}
