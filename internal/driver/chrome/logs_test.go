package chrome

import (
	"testing"

	"github.com/chromedp/cdproto/runtime"
)

// Every console verb has to land on the logcat scale driver.LogEntry declares:
// the runner fetches at "E" and the default properties count entries whose
// level equals "E", so a level spelled any other way is an error the spec never
// sees. A verb with no mapping is info, which is honest about severity without
// fabricating an error the page never logged.
func TestConsoleLevel(t *testing.T) {
	cases := map[runtime.APIType]string{
		runtime.APITypeError:          "E",
		runtime.APITypeAssert:         "E",
		runtime.APITypeWarning:        "W",
		runtime.APITypeDebug:          "D",
		runtime.APITypeLog:            "I",
		runtime.APITypeInfo:           "I",
		runtime.APITypeTable:          "I",
		runtime.APIType("countReset"): "I",
	}
	for apiType, want := range cases {
		if got := consoleLevel(apiType); got != want {
			t.Errorf("consoleLevel(%q) = %q, want %q", apiType, got, want)
		}
	}
}

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
