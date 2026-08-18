//go:build browser

package browser_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The nothing-to-tap fixture is a page holding one line of static text, so the
// seeded generator finds no candidate on any step and the run ends having
// driven the app zero times. It is the shape a provider outage gives a model
// arm, reproduced without a provider.
const deadRunFixture = "nothing-to-tap"

// TestBrowserDeadRunExitCodes drives the real binary against that page and
// reads the status the shell would: the refusal is only worth what the process
// exit code says, and CI, campaign and the analysis all read it rather than the
// error value.
func TestBrowserDeadRunExitCodes(t *testing.T) {
	binary := buildBinary(t, "./cmd/sanderling")
	pageURL := servePage(t)

	for _, testCase := range []struct {
		name     string
		spec     string
		flags    []string
		wantExit int
		wantSays string
	}{
		{
			name:     "a run that explored nothing is refused",
			spec:     "spec.ts",
			wantExit: 1,
			wantSays: "the action generator drove the app in none of them",
		},
		{
			name:     "the property-free flag waives the refusal it names and no other",
			spec:     "spec.ts",
			flags:    []string{"--allow-no-properties"},
			wantExit: 1,
			wantSays: "the action generator drove the app in none of them",
		},
		{
			name:     "the dead-run flag waives the dead-run refusal",
			spec:     "spec.ts",
			flags:    []string{"--allow-no-generator-actions"},
			wantExit: 0,
		},
		{
			// The campaign case: no flags are passed there, and a run that
			// recorded a violation has produced the verdict the refusal exists
			// to demand.
			name:     "a recorded violation carries the run without any flag",
			spec:     "violated-spec.ts",
			wantExit: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			arguments := append(runFlags(t, testCase.spec, pageURL), testCase.flags...)
			exitCode, output := runBinary(t, binary, arguments)

			if exitCode != testCase.wantExit {
				t.Fatalf("exit code: got %d, want %d\n%s", exitCode, testCase.wantExit, output)
			}
			if testCase.wantSays != "" && !strings.Contains(output, testCase.wantSays) {
				t.Errorf("the run never said %q:\n%s", testCase.wantSays, output)
			}
			if testCase.wantExit != 0 && !strings.Contains(output, "--allow-no-generator-actions") {
				t.Errorf("the refusal never names the flag that waives it:\n%s", output)
			}
		})
	}
}

// TestBrowserCampaignRecordsAViolationFoundWithoutTheGenerator follows the
// whole chain a campaign is read through. A campaign passes no
// --exit-on-violation, so a refusal here writes exit_code 1 and the analysis
// drops the run as missing data: the detection the campaign exists to count
// disappears into the excluded pile.
func TestBrowserCampaignRecordsAViolationFoundWithoutTheGenerator(t *testing.T) {
	sanderling := buildBinary(t, "./cmd/sanderling")
	campaign := buildBinary(t, "./cmd/internal-tools/campaign")
	pageURL := servePage(t)
	campaignDirectory := filepath.Join(t.TempDir(), "campaign")

	exitCode, output := runBinary(t, campaign, []string{
		"--spec", fixtureSpec(t, "violated-spec.ts"),
		"--bundle-id", pageURL,
		"--platform", "web",
		"--arm", "dead-run",
		"--generator", "seeded",
		"--max-steps", "3",
		"--duration", "60s",
		"--seeds", "1",
		"--sanderling", sanderling,
		"--output", campaignDirectory,
	})
	if exitCode != 0 {
		t.Fatalf("campaign exit code: got %d, want 0\n%s", exitCode, output)
	}

	body, err := os.ReadFile(filepath.Join(campaignDirectory, "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		ExitCode           int      `json:"exit_code"`
		Actions            int      `json:"actions"`
		ViolatedProperties []string `json:"violated_properties"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("runs.jsonl: %v\n%s", err, body)
	}

	if record.ExitCode != 0 {
		t.Errorf("exit_code: got %d, want 0. The analysis excludes a non-zero record as "+
			"missing data, so this violation would never reach the survival analysis", record.ExitCode)
	}
	if record.Actions != 0 {
		t.Fatalf("actions: got %d, want 0: the fixture offers the generator nothing to drive", record.Actions)
	}
	if len(record.ViolatedProperties) != 1 || record.ViolatedProperties[0] != "bannerIsEmpty" {
		t.Errorf("violated_properties: got %v, want [bannerIsEmpty]", record.ViolatedProperties)
	}
}

func runFlags(t *testing.T, spec, pageURL string) []string {
	t.Helper()
	return []string{
		"test",
		"--platform", "web",
		"--spec", fixtureSpec(t, spec),
		"--bundle-id", pageURL,
		"--duration", "60s",
		"--max-steps", "3",
		"--seed", "1",
		"--output", t.TempDir(),
	}
}

func fixtureSpec(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(testdataDir(t), deadRunFixture, name)
}

func servePage(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir(t))))
	t.Cleanup(server.Close)
	return server.URL + "/" + deadRunFixture + "/"
}

func buildBinary(t *testing.T, packagePath string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), filepath.Base(packagePath))
	build := exec.Command("go", "build", "-o", binary, packagePath)
	build.Dir = repoRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
	return binary
}

func runBinary(t *testing.T, binary string, arguments []string) (int, string) {
	t.Helper()
	command := exec.Command(binary, arguments...)
	command.Dir = repoRoot(t)
	output, err := command.CombinedOutput()
	if command.ProcessState == nil {
		t.Fatalf("%s never ran: %v", binary, err)
	}
	return command.ProcessState.ExitCode(), string(output)
}
