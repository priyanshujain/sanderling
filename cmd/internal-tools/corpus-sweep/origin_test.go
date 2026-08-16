package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// collisionPairs are implementations in this corpus that write the same
// localStorage key as each other. Served from one origin they share one stored
// record: in the corpus survey angular2_es2015 crashed at bootstrap on a record
// angular2 had written, which is a violation belonging to no implementation.
var collisionPairs = [][2]string{
	{"angular2", "angular2_es2015"},
	{"backbone", "backbone_require"},
	{"canjs", "canjs_require"},
	{"react", "typescript-react"},
}

func originOf(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return parsed.Scheme + "://" + parsed.Host
}

// The whole population is swept so the assertion covers every implementation
// rather than the four pairs already known to collide: the survey found those
// four, and an unexamined fifth would be just as damaging.
func TestSweep_GivesEveryImplementationAnOriginNoOtherImplementationShares(
	t *testing.T,
) {
	root := t.TempDir()
	corpus := wholeCorpus(t)
	specPath := filepath.Join(root, "todo.ts")
	if err := os.WriteFile(specPath, []byte("export const properties = [];\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "campaigns")
	campaignLog := filepath.Join(root, "campaign.log")
	fetchLog := filepath.Join(root, "fetch.log")
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	campaignPath := writeScript(
		t,
		filepath.Join(root, "stub-campaign"),
		fmt.Sprintf(
			stubCampaign,
			filepath.Join(output, manifestFileName),
			campaignLog,
			fetchLog,
			testBinary,
		),
	)
	sanderlingPath := writeScript(
		t,
		filepath.Join(root, "stub-sanderling"),
		"#!/bin/sh\nexit 0\n",
	)

	err = run([]string{
		"--corpus", corpus,
		"--spec", specPath,
		"--seeds", "1",
		"--max-steps", "10",
		"--duration", "30s",
		"--concurrency", "8",
		"--base-port", fmt.Sprint(freePortRange(t, len(includedImplementations))),
		"--output", output,
		"--campaign", campaignPath,
		"--sanderling", sanderlingPath,
	}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// What the sweep declared it would serve each implementation from.
	recorded := readManifest(t, filepath.Join(output, manifestFileName))
	if len(recorded.Implementations) != len(includedImplementations) {
		t.Fatalf(
			"intended implementations: got %d, want %d",
			len(recorded.Implementations),
			len(includedImplementations),
		)
	}
	plannedOrigin := map[string]string{}
	ownerOfOrigin := map[string]string{}
	for _, planned := range recorded.Implementations {
		if owner, taken := ownerOfOrigin[planned.Origin]; taken {
			t.Errorf(
				"%s and %s are both served from %s, so they share one localStorage",
				owner,
				planned.Name,
				planned.Origin,
			)
		}
		ownerOfOrigin[planned.Origin] = planned.Name
		plannedOrigin[planned.Name] = planned.Origin
		if got := originOf(t, planned.URL); got != planned.Origin {
			t.Errorf(
				"%s: url %q is not under the origin %q the manifest claims",
				planned.Name,
				planned.URL,
				planned.Origin,
			)
		}
	}
	for _, pair := range collisionPairs {
		if plannedOrigin[pair[0]] == plannedOrigin[pair[1]] {
			t.Errorf(
				"%s and %s write the same localStorage key and are both served from %s",
				pair[0],
				pair[1],
				plannedOrigin[pair[0]],
			)
		}
	}

	// What actually reached the driver. The web driver parses the origin to
	// clear out of --bundle-id, so two arms sharing a bundle-id origin clear
	// and repopulate one another's storage however carefully they are labelled.
	drivenOrigin := map[string]string{}
	for _, line := range readLines(t, campaignLog) {
		arguments := strings.Fields(strings.SplitN(line, "argv: ", 2)[1])
		arm := argumentValue(arguments, "--arm")
		if arm == "" {
			t.Fatalf(
				"a campaign ran with no arm, so its runs cannot be attributed: %q",
				line,
			)
		}
		drivenOrigin[arm] = originOf(t, argumentValue(arguments, "--bundle-id"))
	}
	if len(drivenOrigin) != len(includedImplementations) {
		t.Fatalf(
			"arms that reached the campaign tool: got %d, want %d",
			len(drivenOrigin),
			len(includedImplementations),
		)
	}
	armOfOrigin := map[string]string{}
	for arm, origin := range drivenOrigin {
		if other, taken := armOfOrigin[origin]; taken {
			t.Errorf(
				"arms %s and %s were both driven at %s",
				other,
				arm,
				origin,
			)
		}
		armOfOrigin[origin] = arm
		if origin != plannedOrigin[arm] {
			t.Errorf(
				"%s was driven at %s but the manifest promised %s",
				arm,
				origin,
				plannedOrigin[arm],
			)
		}
	}

	// And what each origin answered with, which is the check that the ports
	// are not merely distinct but each carries its own implementation.
	served := map[string]string{}
	for _, line := range readLines(t, fetchLog) {
		requested, body, found := strings.Cut(line, " -> 200 ")
		if !found {
			t.Errorf("a served page did not answer: %q", line)
			continue
		}
		served[originOf(t, requested)] = body
	}
	for arm, origin := range drivenOrigin {
		if want := "<title>" + arm + "</title>"; !strings.Contains(
			served[origin],
			want,
		) {
			t.Errorf(
				"%s at %s was served %q, which is not its own document",
				arm,
				origin,
				served[origin],
			)
		}
	}
}
