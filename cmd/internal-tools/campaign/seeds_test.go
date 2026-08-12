package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParseSeeds_RangesAndLists(t *testing.T) {
	cases := []struct {
		specification string
		want          []int64
	}{
		{"1-5", []int64{1, 2, 3, 4, 5}},
		{"1,5,9", []int64{1, 5, 9}},
		{"1-3,20,30-32", []int64{1, 2, 3, 20, 30, 31, 32}},
		{" 7 , 8 ", []int64{7, 8}},
		{"4-4", []int64{4}},
	}
	for _, testCase := range cases {
		got, err := parseSeeds(testCase.specification)
		if err != nil {
			t.Fatalf("%q: %v", testCase.specification, err)
		}
		if !slices.Equal(got, testCase.want) {
			t.Errorf("%q: got %v, want %v", testCase.specification, got, testCase.want)
		}
	}
}

func TestParseSeeds_RejectsSeedZero(t *testing.T) {
	for _, specification := range []string{"0", "1,0,2", "0-3"} {
		_, err := parseSeeds(specification)
		if err == nil {
			t.Fatalf("%q: expected rejection of seed 0", specification)
		}
		if !strings.Contains(err.Error(), "not reproducible") {
			t.Errorf("%q: error should explain why seed 0 is rejected: %v", specification, err)
		}
	}
}

func TestParseSeeds_RejectsMalformed(t *testing.T) {
	for _, specification := range []string{"", "   ", "abc", "1,,2", "5-1", "1-", "-5", "1-2-3", "1.5", "2,2"} {
		if seeds, err := parseSeeds(specification); err == nil {
			t.Errorf("%q: expected error, got %v", specification, seeds)
		}
	}
}
