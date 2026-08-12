package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubSanderling answers `version`, writes a run directory shaped like the one
// `sanderling test` produces, violates on seed 5 and fails on seed 7.
const stubSanderling = `#!/bin/sh
if [ "$1" = "version" ]; then
  echo "stub 9.9.9"
  exit 0
fi
seed=""
output=""
while [ $# -gt 0 ]; do
  case "$1" in
    --seed) seed="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "stub run seed=$seed"
run="$output/20260101-000000"
mkdir -p "$run"
echo "{\"seed\":$seed,\"platform\":\"web\"}" > "$run/meta.json"
{
  echo '{"step":1,"hierarchy":{"elements":[]}}'
  echo '{"step":2,"hierarchy":{"elements":[]}}'
  if [ "$seed" = "5" ]; then
    echo '{"step":3,"hierarchy":{"elements":[]},"violations":["cartTotalMatches"],"witnesses":{"cartTotalMatches":{"reason":"total drifted","step":2,"detected_step":3}}}'
  else
    echo '{"step":3,"hierarchy":{"elements":[]}}'
  fi
} > "$run/trace.jsonl"
if [ "$seed" = "7" ]; then
  echo "driver crashed" >&2
  exit 3
fi
`

func TestRun_EndToEndAgainstStubBinary(t *testing.T) {
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "stub-sanderling")
	if err := os.WriteFile(binaryPath, []byte(stubSanderling), 0o755); err != nil {
		t.Fatal(err)
	}
	campaignDirectory := filepath.Join(directory, "campaign")
	var stdout bytes.Buffer

	err := run([]string{
		"--spec", "/specs/folio.ts",
		"--bundle-id", "app.folio",
		"--platform", "web",
		"--arm", "seeded-web",
		"--generator", "seeded",
		"--max-steps", "50",
		"--duration", "30s",
		"--seeds", "4-5,7",
		"--devices", "worker-a,worker-b",
		"--sanderling", binaryPath,
		"--output", campaignDirectory,
		"--", "--clear-data=false",
	}, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "1 of 3 runs failed") {
		t.Fatalf("expected the failing seed to be reported, got %v", err)
	}

	body, err := os.ReadFile(filepath.Join(campaignDirectory, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var recorded manifest
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.SanderlingVersion != "stub 9.9.9" {
		t.Errorf("version: got %q", recorded.SanderlingVersion)
	}
	if !slices.Equal(recorded.Seeds, []int64{4, 5, 7}) {
		t.Errorf("intended seeds: got %v", recorded.Seeds)
	}
	if slices.Contains(recorded.ArgumentTemplate, "--device") {
		t.Errorf("web template should carry no device flag: %v", recorded.ArgumentTemplate)
	}
	if recorded.ArgumentTemplate[len(recorded.ArgumentTemplate)-1] != "--clear-data=false" {
		t.Errorf("template lost the passthrough flag: %v", recorded.ArgumentTemplate)
	}

	records := readRecords(t, campaignDirectory)
	if len(records) != 3 {
		t.Fatalf("records: got %d, want 3", len(records))
	}
	bySeed := map[int64]runRecord{}
	for _, record := range records {
		bySeed[record.Seed] = record
	}
	for _, seed := range []int64{4, 5, 7} {
		record, ok := bySeed[seed]
		if !ok {
			t.Fatalf("seed %d is missing from %s", seed, recordsFileName)
		}
		if record.Steps != 3 {
			t.Errorf("seed %d steps: got %d, want 3", seed, record.Steps)
		}
		if record.RunDirectory == "" {
			t.Errorf("seed %d has no run directory", seed)
		}
		if !slices.Contains([]string{"worker-a", "worker-b"}, record.Device) {
			t.Errorf("seed %d device: got %q", seed, record.Device)
		}
	}
	violating := bySeed[5]
	if violating.FirstViolationOriginStep == nil || *violating.FirstViolationOriginStep != 2 {
		t.Fatalf("seed 5 origin step: got %v, want 2", violating.FirstViolationOriginStep)
	}
	if *violating.FirstViolationDetectedStep != 3 || violating.FirstViolationReason != "total drifted" {
		t.Errorf("seed 5 violation: %+v", violating)
	}
	if bySeed[4].FirstViolationOriginStep != nil {
		t.Errorf("seed 4 should be censored at the budget: %+v", bySeed[4])
	}
	if bySeed[7].ExitCode != 3 {
		t.Errorf("seed 7 exit code: got %d, want 3", bySeed[7].ExitCode)
	}

	log, err := os.ReadFile(filepath.Join(campaignDirectory, "seed-7", "sanderling.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "driver crashed") {
		t.Errorf("seed 7 log lost the stderr line: %q", log)
	}
	if !strings.Contains(stdout.String(), "outcome=violation@2") {
		t.Errorf("progress output: %q", stdout.String())
	}
}
