package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the fetcher the stub campaign uses, so the URL a
// campaign is handed is really requested while the sweep is serving, and what
// came back is on disk for the test to read.
func TestMain(m *testing.M) {
	if url := os.Getenv("CORPUS_SWEEP_TEST_FETCH_URL"); url != "" {
		recordFetch(url, os.Getenv("CORPUS_SWEEP_TEST_FETCH_LOG"))
		return
	}
	os.Exit(m.Run())
}

func recordFetch(url, logPath string) {
	line := ""
	response, err := http.Get(url)
	if err != nil {
		line = fmt.Sprintf("%s -> error %v\n", url, err)
	} else {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		line = fmt.Sprintf("%s -> %d %s\n", url, response.StatusCode, body)
	}
	logFile, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return
	}
	logFile.WriteString(line)
	logFile.Close()
}

// stubCampaign records the argv it was handed and whether the sweep manifest
// was already on disk when it ran, fetches the URL it was told to drive, writes
// the campaign directory the real tool would write, and fails one seed.
const stubCampaign = `#!/bin/sh
output=""
seed=""
url=""
previous=""
for argument in "$@"; do
  case "$previous" in
    --output) output="$argument" ;;
    --seeds) seed="$argument" ;;
    --bundle-id) url="$argument" ;;
  esac
  previous="$argument"
done
manifest=missing
if [ -f "%[1]s" ]; then manifest=present; fi
echo "manifest=$manifest argv: $*" >> "%[2]s"
CORPUS_SWEEP_TEST_FETCH_URL="$url" CORPUS_SWEEP_TEST_FETCH_LOG="%[3]s" "%[4]s"
mkdir -p "$output"
printf '{"seeds":[%%s]}\n' "$seed" > "$output/campaign.json"
echo "stub campaign seed=$seed url=$url"
case "$output" in
  *angular2_es2015/seed-5) exit 1 ;;
esac
exit 0
`

func TestRun_EndToEndAgainstAServedCorpusAndAStubCampaign(t *testing.T) {
	root := t.TempDir()
	corpus := wholeCorpus(t)
	// dojo's document is served but cannot be read, so it stalls at serve and
	// the two implementations either side of it still have to reach the
	// campaign tool with both seeds.
	unreadable := filepath.Join(corpus, filepath.FromSlash(documentFor("dojo")))
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })

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
	selected := []string{"angular2", "angular2_es2015", "dojo"}
	basePort := freePortRange(t, len(selected))

	var stdout bytes.Buffer
	err = run([]string{
		"--corpus", corpus,
		"--spec", specPath,
		"--implementations", strings.Join(selected, ","),
		"--seeds", "4-5",
		"--max-steps", "40",
		"--duration", "30s",
		"--concurrency", "2",
		"--base-port", fmt.Sprint(basePort),
		"--output", output,
		"--campaign", campaignPath,
		"--sanderling", sanderlingPath,
	}, &stdout, io.Discard)
	if err == nil ||
		!strings.Contains(err.Error(), "1 of 3 implementations never ran") {
		t.Fatalf(
			"expected the stalled implementation and the failed campaign to be reported, got %v",
			err,
		)
	}

	recorded := readManifest(t, filepath.Join(output, manifestFileName))
	if !slices.Equal(recorded.Seeds, []int64{4, 5}) {
		t.Errorf("intended seeds: got %v", recorded.Seeds)
	}
	if len(recorded.Implementations) != len(selected) {
		t.Fatalf("intended implementations: got %v", recorded.Implementations)
	}
	for index, planned := range recorded.Implementations {
		if planned.Name != selected[index] {
			t.Errorf(
				"implementation %d: got %q, want %q",
				index,
				planned.Name,
				selected[index],
			)
		}
		wantPort := basePort + index
		wantURL := fmt.Sprintf(
			"http://127.0.0.1:%d/examples/%s/index.html",
			wantPort,
			planned.Name,
		)
		if planned.Port != wantPort || planned.URL != wantURL {
			t.Errorf(
				"%s: port %d url %q, want %d %q",
				planned.Name,
				planned.Port,
				planned.URL,
				wantPort,
				wantURL,
			)
		}
	}
	if recorded.Generator != "seeded" || recorded.Platform != "web" ||
		recorded.MaxSteps != 40 {
		t.Errorf(
			"manifest generator/platform/budget: %q/%q/%d",
			recorded.Generator,
			recorded.Platform,
			recorded.MaxSteps,
		)
	}
	if recorded.CorpusRoot != corpus {
		t.Errorf(
			"manifest corpus root: got %q, want %q",
			recorded.CorpusRoot,
			corpus,
		)
	}

	campaignLines := readLines(t, campaignLog)
	if len(campaignLines) != 4 {
		t.Fatalf(
			"campaign invocations: got %d, want 4:\n%s",
			len(campaignLines),
			strings.Join(campaignLines, "\n"),
		)
	}
	seen := map[string]bool{}
	for _, line := range campaignLines {
		if !strings.HasPrefix(line, "manifest=present") {
			t.Errorf(
				"a campaign ran before the sweep manifest was written: %q",
				line,
			)
		}
		arguments := strings.Fields(strings.SplitN(line, "argv: ", 2)[1])
		arm := argumentValue(arguments, "--arm")
		seed := argumentValue(arguments, "--seeds")
		port := basePort + slices.Index(selected, arm)
		wantBundle := fmt.Sprintf(
			"http://127.0.0.1:%d/examples/%s/index.html",
			port,
			arm,
		)
		if got := argumentValue(arguments, "--bundle-id"); got != wantBundle {
			t.Errorf(
				"%s seed %s: bundle id %q, want %q",
				arm,
				seed,
				got,
				wantBundle,
			)
		}
		if got := argumentValue(arguments, "--output"); got != filepath.Join(
			output,
			arm,
			"seed-"+seed,
		) {
			t.Errorf("%s seed %s: campaign output %q", arm, seed, got)
		}
		if got := argumentValue(arguments, "--sanderling"); got != sanderlingPath {
			t.Errorf("%s seed %s: sanderling path %q", arm, seed, got)
		}
		seen[arm+"/"+seed] = true
	}
	for _, want := range []string{"angular2/4", "angular2/5", "angular2_es2015/4", "angular2_es2015/5"} {
		if !seen[want] {
			t.Errorf("%s never reached the campaign tool", want)
		}
	}

	// What the served page actually returned: each port answered with its own
	// implementation's document, so no arm was driven against another's.
	fetched := readLines(t, fetchLog)
	for index, name := range []string{"angular2", "angular2_es2015"} {
		want := fmt.Sprintf(
			"http://127.0.0.1:%d/examples/%s/index.html -> 200 <!doctype html><title>%s</title>",
			basePort+index,
			name,
			name,
		)
		matched := 0
		for _, line := range fetched {
			if strings.HasPrefix(line, want) {
				matched++
			}
		}
		if matched != 2 {
			t.Errorf(
				"%s was served its own document %d times, want 2:\n%s",
				name,
				matched,
				strings.Join(fetched, "\n"),
			)
		}
	}

	records := readRecords(t, filepath.Join(output, recordsFileName))
	if len(records) != 3 {
		t.Fatalf("implementation records: got %d, want 3", len(records))
	}
	byName := map[string]implementationRecord{}
	for _, record := range records {
		byName[record.Name] = record
	}
	stalled := byName["dojo"]
	if stalled.FailedStage != stageServe || len(stalled.Runs) != 0 {
		t.Errorf(
			"dojo: stage %q with %d runs, want a serve failure and no runs",
			stalled.FailedStage,
			len(stalled.Runs),
		)
	}
	for _, name := range []string{"angular2", "angular2_es2015"} {
		record := byName[name]
		if record.FailedStage != "" || len(record.Runs) != 2 {
			t.Errorf(
				"%s: stage %q with %d runs, want no failure and 2 runs",
				name,
				record.FailedStage,
				len(record.Runs),
			)
		}
		if record.MonotonicMillis <= 0 {
			t.Errorf(
				"%s took %d ms, so nothing timed how long it worked",
				name,
				record.MonotonicMillis,
			)
		}
	}
	if exit := byName["angular2_es2015"].Runs[1].ExitCode; exit != 1 {
		t.Errorf("angular2_es2015 seed 5 exit code: got %d, want 1", exit)
	}
	if exit := byName["angular2_es2015"].Runs[0].ExitCode; exit != 0 {
		t.Errorf("angular2_es2015 seed 4 exit code: got %d, want 0", exit)
	}

	for _, name := range []string{"angular2", "angular2_es2015"} {
		for _, seed := range []string{"4", "5"} {
			directory := filepath.Join(output, name, "seed-"+seed)
			if _, err := os.Stat(filepath.Join(directory, "campaign.json")); err != nil {
				t.Errorf(
					"%s seed %s: no campaign directory: %v",
					name,
					seed,
					err,
				)
			}
			log, err := os.ReadFile(filepath.Join(directory, "campaign.log"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(log), "stub campaign seed="+seed) {
				t.Errorf(
					"%s seed %s: campaign output was not captured: %q",
					name,
					seed,
					log,
				)
			}
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	for offset := range selected {
		if response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", basePort+offset)); err == nil {
			response.Body.Close()
			t.Errorf(
				"port %d is still served after the sweep finished",
				basePort+offset,
			)
		}
	}
	if !strings.Contains(stdout.String(), "failed at serve") {
		t.Errorf(
			"progress output does not name the stalled implementation: %q",
			stdout.String(),
		)
	}
}

func TestRun_RefusesAnOutputDirectoryThatAlreadyHoldsASweep(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "campaigns")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, manifestFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, "todo.ts")
	if err := os.WriteFile(specPath, []byte("export const properties = [];\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{
		"--corpus", wholeCorpus(t), "--spec", specPath, "--seeds", "1",
		"--max-steps", "10", "--output", output,
	}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), manifestFileName) {
		t.Fatalf(
			"two sweeps sharing a directory would interleave their records, got %v",
			err,
		)
	}
}

func writeScript(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func readManifest(t *testing.T, path string) manifest {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recorded manifest
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	return recorded
}

func readRecords(t *testing.T, path string) []implementationRecord {
	t.Helper()
	var records []implementationRecord
	for _, line := range readLines(t, path) {
		var record implementationRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func argumentValue(arguments []string, name string) string {
	index := slices.Index(arguments, name)
	if index < 0 || index+1 >= len(arguments) {
		return ""
	}
	return arguments[index+1]
}

// freePortRange finds count consecutive free ports, which is what the sweep
// hands out: one port per implementation from --base-port upwards.
func freePortRange(t *testing.T, count int) int {
	t.Helper()
	for range 100 {
		base := 20000 + rand.Intn(20000)
		if portsAreFree(base, count) {
			return base
		}
	}
	t.Fatalf("no run of %d free ports", count)
	return 0
}

func portsAreFree(base, count int) bool {
	for offset := range count {
		listener, err := net.Listen(
			"tcp",
			fmt.Sprintf("127.0.0.1:%d", base+offset),
		)
		if err != nil {
			return false
		}
		listener.Close()
	}
	return true
}
