package main

// Published right-censored datasets whose log-rank and Kaplan-Meier results are
// reported in the survival-analysis literature and in R's survival package, so
// every expected number in these tests can be checked against a source rather
// than against this tool's own output.

// gehanSixMercaptopurine and gehanPlacebo are remission times in weeks from the
// 6-MP versus placebo trial in acute leukaemia, Freireich et al. (1963). This is
// the dataset R's survival literature calls gehan. A trailing plus in the
// published listing marks a censored time.
//
//	6-MP:    6, 6, 6, 6+, 7, 9+, 10, 10+, 11+, 13, 16, 17+, 19+, 20+, 22, 23, 25+, 32+, 32+, 34+, 35+
//	placebo: 1, 1, 2, 2, 3, 4, 4, 5, 5, 8, 8, 8, 8, 11, 11, 12, 12, 15, 17, 22, 23
var (
	gehanSixMercaptopurine = []observation{
		{6, true}, {6, true}, {6, true}, {6, false},
		{7, true}, {9, false}, {10, true}, {10, false},
		{11, false}, {13, true}, {16, true}, {17, false},
		{19, false}, {20, false}, {22, true}, {23, true},
		{25, false}, {32, false}, {32, false}, {34, false}, {35, false},
	}
	gehanPlacebo = []observation{
		{1, true}, {1, true}, {2, true}, {2, true}, {3, true},
		{4, true}, {4, true}, {5, true}, {5, true}, {8, true},
		{8, true}, {8, true}, {8, true}, {11, true}, {11, true},
		{12, true}, {12, true}, {15, true}, {17, true}, {22, true}, {23, true},
	}
)

// amlMaintained and amlNonmaintained are the acute myelogenous leukaemia
// survival times in weeks from Miller (1997), shipped as the aml dataset in R's
// survival package. Five subjects are censored, at 13, 16, 28, 45 and 161 weeks.
//
//	maintained:    9, 13, 13+, 18, 23, 28+, 31, 34, 45+, 48, 161+
//	nonmaintained: 5, 5, 8, 8, 12, 16+, 23, 27, 30, 33, 43, 45
var (
	amlMaintained = []observation{
		{9, true}, {13, true}, {13, false}, {18, true}, {23, true},
		{28, false}, {31, true}, {34, true}, {45, false}, {48, true}, {161, false},
	}
	amlNonmaintained = []observation{
		{5, true}, {5, true}, {8, true}, {8, true}, {12, true}, {16, false},
		{23, true}, {27, true}, {30, true}, {33, true}, {43, true}, {45, true},
	}
)

// chorioamnionTerm and chorioamnionEarly are permeability constants of the human
// chorioamnion at term and between 12 and 26 weeks gestational age, Hollander
// and Wolfe (1973), 69f. R's wilcox.test help page uses exactly these vectors as
// its two-sample example.
var (
	chorioamnionTerm  = []float64{0.80, 0.83, 1.89, 1.04, 1.45, 1.38, 1.91, 1.64, 0.73, 1.46}
	chorioamnionEarly = []float64{1.15, 0.88, 0.90, 0.74, 1.21}
)
