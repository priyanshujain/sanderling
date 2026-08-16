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

// The test binary doubles as the stub implementation's server and as the
// fetcher the stub campaign uses, so the sweep drives a real preview process
// over a real port and the served URL is answered by a real HTTP server.
func TestMain(m *testing.M) {
	switch {
	case os.Getenv("SWEEP_TEST_SERVE_PORT") != "":
		serveUntilKilled(os.Getenv("SWEEP_TEST_SERVE_PORT"))
	case os.Getenv("SWEEP_TEST_FETCH_URL") != "":
		recordFetch(
			os.Getenv("SWEEP_TEST_FETCH_URL"),
			os.Getenv("SWEEP_TEST_FETCH_LOG"),
		)
	default:
		os.Exit(m.Run())
	}
}

func serveUntilKilled(port string) {
	handler := http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			fmt.Fprintf(writer, "%s %s", port, request.URL.RequestURI())
		},
	)
	http.ListenAndServe("localhost:"+port, handler)
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

// stubBun answers install, fails to build impl-02, and serves the preview from
// the test binary on the port it was given.
const stubBun = `#!/bin/sh
echo "$PWD $*" >> "%[1]s"
if [ "$1" = "run" ] && [ "$2" = "build" ]; then
  case "$PWD" in *impl-02) echo "TS2322: type error" >&2; exit 1 ;; esac
  exit 0
fi
if [ "$1" = "run" ] && [ "$2" = "preview" ]; then
  port=""
  previous=""
  for argument in "$@"; do
    if [ "$previous" = "--port" ]; then port="$argument"; fi
    previous="$argument"
  done
  SWEEP_TEST_SERVE_PORT="$port" exec "%[2]s"
fi
exit 0
`

// stubCampaign records the argv it was handed and whether the sweep manifest
// was already on disk when it ran, fetches the URL it was told to drive, writes
// the campaign directory the real tool would write, and fails impl-03 seed 4.
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
SWEEP_TEST_FETCH_URL="$url" SWEEP_TEST_FETCH_LOG="%[3]s" "%[4]s"
mkdir -p "$output"
printf '{"arm":"stub","seeds":[%%s]}\n' "$seed" > "$output/campaign.json"
echo "stub campaign seed=$seed url=$url"
case "$output" in
  *impl-03/seed-4) exit 1 ;;
esac
exit 0
`

func TestRun_EndToEndAgainstStubBunAndCampaign(t *testing.T) {
	root := t.TempDir()
	implementations := filepath.Join(root, "implementations")
	for _, name := range []string{"impl-01", "impl-02", "impl-03"} {
		if err := os.MkdirAll(filepath.Join(implementations, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	specPath := filepath.Join(root, "relay.ts")
	if err := os.WriteFile(specPath, []byte("export const properties = [];\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "campaigns")
	bunLog := filepath.Join(root, "bun.log")
	campaignLog := filepath.Join(root, "campaign.log")
	fetchLog := filepath.Join(root, "fetch.log")
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	bunPath := writeScript(
		t,
		filepath.Join(root, "stub-bun"),
		fmt.Sprintf(stubBun, bunLog, testBinary),
	)
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
	basePort := freePortRange(t, 3)

	var stdout bytes.Buffer
	err = run([]string{
		"--implementations", implementations,
		"--spec", specPath,
		"--seeds", "4-5",
		"--max-steps", "40",
		"--duration", "30s",
		"--concurrency", "2",
		"--base-port", fmt.Sprint(basePort),
		"--output", output,
		"--bun", bunPath,
		"--campaign", campaignPath,
		"--sanderling", sanderlingPath,
	}, &stdout, io.Discard)
	if err == nil ||
		!strings.Contains(err.Error(), "1 of 3 implementations never ran") {
		t.Fatalf(
			"expected the failed build and the failed campaign to be reported, got %v",
			err,
		)
	}

	var recorded manifest
	manifestBody, err := os.ReadFile(filepath.Join(output, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBody, &recorded); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(recorded.Seeds, []int64{4, 5}) {
		t.Errorf("intended seeds: got %v", recorded.Seeds)
	}
	if len(recorded.Implementations) != 3 {
		t.Fatalf("intended implementations: got %v", recorded.Implementations)
	}
	for index, planned := range recorded.Implementations {
		wantPort := basePort + index
		if planned.Port != wantPort {
			t.Errorf(
				"%s port: got %d, want %d",
				planned.Name,
				planned.Port,
				wantPort,
			)
		}
		wantURL := fmt.Sprintf("http://localhost:%d/?seed={seed}", wantPort)
		if planned.URLTemplate != wantURL {
			t.Errorf(
				"%s url template: got %q, want %q",
				planned.Name,
				planned.URLTemplate,
				wantURL,
			)
		}
	}
	if recorded.Generator != "seeded" || recorded.MaxSteps != 40 {
		t.Errorf(
			"manifest generator/budget: got %q/%d",
			recorded.Generator,
			recorded.MaxSteps,
		)
	}

	// impl-02 fails its build, so the two implementations either side of it
	// still have to reach the campaign tool with both seeds.
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
		bundle := argumentValue(arguments, "--bundle-id")
		port := basePort + slices.Index(
			[]string{"impl-01", "impl-02", "impl-03"},
			arm,
		)
		wantBundle := fmt.Sprintf("http://localhost:%d/?seed=%s", port, seed)
		if bundle != wantBundle {
			t.Errorf(
				"%s seed %s: bundle id %q, want %q",
				arm,
				seed,
				bundle,
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
	for _, want := range []string{"impl-01/4", "impl-01/5", "impl-03/4", "impl-03/5"} {
		if !seen[want] {
			t.Errorf("%s never reached the campaign tool", want)
		}
	}

	// What the served page actually saw: the right port for the
	// implementation, carrying the same seed the campaign was given.
	fetched := readLines(t, fetchLog)
	for _, want := range []string{
		fmt.Sprintf("http://localhost:%d/?seed=4 -> 200 %d /?seed=4", basePort, basePort),
		fmt.Sprintf("http://localhost:%d/?seed=5 -> 200 %d /?seed=5", basePort, basePort),
		fmt.Sprintf("http://localhost:%d/?seed=4 -> 200 %d /?seed=4", basePort+2, basePort+2),
		fmt.Sprintf("http://localhost:%d/?seed=5 -> 200 %d /?seed=5", basePort+2, basePort+2),
	} {
		if !slices.Contains(fetched, want) {
			t.Errorf(
				"the served page never saw %q:\n%s",
				want,
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
	failed := byName["impl-02"]
	if failed.FailedStage != stageBuild || len(failed.Runs) != 0 {
		t.Errorf(
			"impl-02: got stage %q with %d runs, want a build failure and no runs",
			failed.FailedStage,
			len(failed.Runs),
		)
	}
	if !strings.Contains(failed.Error, "build.log") {
		t.Errorf("impl-02 error should point at its log: %q", failed.Error)
	}
	buildLog, err := os.ReadFile(
		filepath.Join(output, "impl-02", stageBuild+".log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buildLog), "TS2322") {
		t.Errorf("impl-02 build log lost the compiler error: %q", buildLog)
	}
	for _, name := range []string{"impl-01", "impl-03"} {
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
		for _, run := range record.Runs {
			if run.MonotonicMillis <= 0 {
				t.Errorf(
					"%s seed %d took %d ms, so nothing timed the campaign",
					name,
					run.Seed,
					run.MonotonicMillis,
				)
			}
		}
	}
	if exit := byName["impl-03"].Runs[0].ExitCode; exit != 1 {
		t.Errorf("impl-03 seed 4 exit code: got %d, want 1", exit)
	}
	if exit := byName["impl-03"].Runs[1].ExitCode; exit != 0 {
		t.Errorf(
			"impl-03 seed 5 ran after seed 4 failed and should have exited 0, got %d",
			exit,
		)
	}

	for _, name := range []string{"impl-01", "impl-03"} {
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

	installed := readLines(t, bunLog)
	for _, name := range []string{"impl-01", "impl-02", "impl-03"} {
		if !slices.Contains(
			installed,
			filepath.Join(implementations, name)+" install",
		) {
			t.Errorf(
				"%s was never installed:\n%s",
				name,
				strings.Join(installed, "\n"),
			)
		}
	}

	// Every preview server the sweep started is gone with it: a leaked one
	// holds its port, and the next sweep would be served by the old build.
	client := &http.Client{Timeout: 2 * time.Second}
	for _, port := range []int{basePort, basePort + 2} {
		if response, err := client.Get(readinessURL(port)); err == nil {
			response.Body.Close()
			t.Errorf("port %d is still served after the sweep finished", port)
		}
	}
	if !strings.Contains(stdout.String(), "failed at build") {
		t.Errorf(
			"progress output does not name the build failure: %q",
			stdout.String(),
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
			fmt.Sprintf("localhost:%d", base+offset),
		)
		if err != nil {
			return false
		}
		listener.Close()
	}
	return true
}
