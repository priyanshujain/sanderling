package chrome

import "testing"

// A level the scale does not name is unknown, not verbose. Ranking it below
// every threshold is what silently emptied the web log channel: the entries
// existed, the filter dropped them, and the run reported nothing. Evidence the
// filter cannot rank has to reach the caller, who can at least see it.
func TestMeetsLevel(t *testing.T) {
	cases := []struct {
		level    string
		minLevel string
		want     bool
	}{
		{"E", "E", true},
		{"F", "E", true},
		{"W", "E", false},
		{"I", "E", false},
		{"D", "E", false},
		{"V", "E", false},
		{"W", "W", true},
		{"I", "W", false},
		{"D", "V", true},
		{"ERROR", "E", true},
		{"WARNING", "E", true},
		{"", "E", true},
	}
	for _, tc := range cases {
		if got := meetsLevel(tc.level, tc.minLevel); got != tc.want {
			t.Errorf("meetsLevel(%q, %q) = %v, want %v", tc.level, tc.minLevel, got, tc.want)
		}
	}
}
