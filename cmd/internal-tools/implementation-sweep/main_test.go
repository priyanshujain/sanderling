package main

import (
	"io"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func baseArguments() []string {
	return []string{
		"--implementations", "/e4/implementations",
		"--spec", "/e4/relay.ts",
		"--seeds", "1-3",
		"--max-steps", "400",
		"--output", "/campaigns/e4",
	}
}

func TestParseArguments_DefaultsAndSeeds(t *testing.T) {
	configuration, err := parseArguments(baseArguments(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(configuration.seeds, []int64{1, 2, 3}) {
		t.Errorf("seeds: got %v", configuration.seeds)
	}
	if configuration.concurrency != defaultConcurrency {
		t.Errorf(
			"concurrency default: got %d, want %d",
			configuration.concurrency,
			defaultConcurrency,
		)
	}
	if configuration.basePort != defaultBasePort {
		t.Errorf(
			"base port default: got %d, want %d",
			configuration.basePort,
			defaultBasePort,
		)
	}
	if configuration.duration != 5*time.Minute {
		t.Errorf("duration default: got %s", configuration.duration)
	}
	for name, got := range map[string]string{
		"bun":        configuration.bunPath,
		"campaign":   configuration.campaignPath,
		"sanderling": configuration.sanderlingPath,
	} {
		if got != name {
			t.Errorf("%s path default: got %q", name, got)
		}
	}
}

func TestParseArguments_Rejections(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		want      string
	}{
		{
			"missing implementations",
			[]string{
				"--spec",
				"s",
				"--seeds",
				"1",
				"--max-steps",
				"10",
				"--output",
				"o",
			},
			"--implementations is required",
		},
		{
			"missing spec",
			[]string{
				"--implementations",
				"i",
				"--seeds",
				"1",
				"--max-steps",
				"10",
				"--output",
				"o",
			},
			"--spec is required",
		},
		{
			"missing output",
			[]string{
				"--implementations",
				"i",
				"--spec",
				"s",
				"--seeds",
				"1",
				"--max-steps",
				"10",
			},
			"--output is required",
		},
		{
			"zero max steps",
			append(baseArguments(), "--max-steps", "0"),
			"--max-steps must be positive",
		},
		{
			"zero concurrency",
			append(baseArguments(), "--concurrency", "0"),
			"--concurrency must be positive",
		},
		{
			"privileged base port",
			append(baseArguments(), "--base-port", "80"),
			"outside 1024-65535",
		},
		{
			"seed zero",
			append(baseArguments(), "--seeds", "0,1"),
			"not reproducible",
		},
	}
	for _, testCase := range cases {
		_, err := parseArguments(testCase.arguments, io.Discard)
		if err == nil {
			t.Errorf("%s: expected error", testCase.name)
			continue
		}
		if !strings.Contains(err.Error(), testCase.want) {
			t.Errorf(
				"%s: got %q, want it to contain %q",
				testCase.name,
				err,
				testCase.want,
			)
		}
	}
}

// Three flags missing is one rerun, not three: the operator is told about all
// of them at once, in flag order, whatever order the check happened to walk.
func TestParseArguments_NamesEveryMissingRequiredFlagInFlagOrder(t *testing.T) {
	_, err := parseArguments(
		[]string{"--spec", "s", "--max-steps", "10"},
		io.Discard,
	)
	if err == nil {
		t.Fatal("got no error, want every missing flag named")
	}
	message := err.Error()
	previous := -1
	for _, name := range []string{"--implementations", "--seeds", "--output"} {
		at := strings.Index(message, name)
		if at < 0 {
			t.Fatalf("got %q, want %s named", message, name)
		}
		if at < previous {
			t.Errorf("got %q, want the flags named in flag order", message)
		}
		previous = at
	}
	if strings.Contains(message, "--spec") {
		t.Errorf("got %q, want the supplied --spec left out", message)
	}
}

// The seed reaches two independent things, the campaign's own seed and the
// scaffold's failure stream, and a replay reproduces neither unless they carry
// the same number.
func TestCampaignArguments_OneSeedReachesBothTheCampaignAndTheURL(
	t *testing.T,
) {
	configuration, err := parseArguments(
		append(baseArguments(), "--", "--clear-data=false"),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := implementation{
		Name:      "impl-07",
		Directory: "/e4/implementations/impl-07",
		Port:      5306,
	}
	for _, seed := range []string{"1", "42"} {
		arguments := campaignArguments(configuration, target, seed)
		if got := argumentValue(arguments, "--seeds"); got != seed {
			t.Errorf("--seeds: got %q, want %q", got, seed)
		}
		bundle := argumentValue(arguments, "--bundle-id")
		parsed, err := url.Parse(bundle)
		if err != nil {
			t.Fatalf("--bundle-id %q: %v", bundle, err)
		}
		if got := parsed.Query().Get("seed"); got != seed {
			t.Errorf(
				"served URL seed: got %q, want %q (from %q)",
				got,
				seed,
				bundle,
			)
		}
		if parsed.Host != "localhost:"+strconv.Itoa(target.Port) {
			t.Errorf(
				"served host: got %q, want the implementation's own port %d",
				parsed.Host,
				target.Port,
			)
		}
		for flagName, want := range map[string]string{
			"--arm":       "impl-07",
			"--platform":  "web",
			"--generator": "seeded",
			"--max-steps": "400",
			"--spec":      "/e4/relay.ts",
			"--output":    filepath.Join("/campaigns/e4", "impl-07", "seed-"+seed),
		} {
			if got := argumentValue(arguments, flagName); got != want {
				t.Errorf("%s: got %q, want %q", flagName, got, want)
			}
		}
		if arguments[len(arguments)-2] != "--" ||
			arguments[len(arguments)-1] != "--clear-data=false" {
			t.Errorf("passthrough flags lost: %v", arguments)
		}
	}
}

func argumentValue(arguments []string, name string) string {
	index := slices.Index(arguments, name)
	if index < 0 || index+1 >= len(arguments) {
		return ""
	}
	return arguments[index+1]
}
