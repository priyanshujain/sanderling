package ioscompanion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildForTestingArgs(t *testing.T) {
	creds := signingCredentials{team: "TEAM1", authKeyPath: "/k/AuthKey.p8", authKeyID: "KID", authIssuerID: "ISS"}
	args := buildForTestingArgs("/p/CompanionRunner.xcodeproj", "/d/derived", creds)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"xcodebuild build-for-testing",
		"-project /p/CompanionRunner.xcodeproj",
		"-scheme CompanionRunner",
		"-destination generic/platform=iOS",
		"-derivedDataPath /d/derived",
		"-allowProvisioningUpdates",
		"-authenticationKeyPath /k/AuthKey.p8",
		"-authenticationKeyID KID",
		"-authenticationKeyIssuerID ISS",
		"CODE_SIGNING_ALLOWED=YES",
		"CODE_SIGNING_REQUIRED=YES",
		"CODE_SIGN_STYLE=Automatic",
		"DEVELOPMENT_TEAM=TEAM1",
		"GENERATE_INFOPLIST_FILE=YES",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("build args missing %q in %q", want, joined)
		}
	}
}

func TestTestWithoutBuildingArgs(t *testing.T) {
	creds := signingCredentials{team: "TEAM1", authKeyPath: "/k/AuthKey.p8", authKeyID: "KID", authIssuerID: "ISS"}
	args := testWithoutBuildingArgs("/x/run.xctestrun", "00008140-HW", creds)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"xcodebuild test-without-building",
		"-xctestrun /x/run.xctestrun",
		"-destination platform=iOS,id=00008140-HW",
		"-allowProvisioningUpdates",
		"-authenticationKeyPath /k/AuthKey.p8",
		"-authenticationKeyID KID",
		"-authenticationKeyIssuerID ISS",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("test args missing %q in %q", want, joined)
		}
	}
}

func TestIproxyArgs(t *testing.T) {
	args := iproxyArgs("49200", "27753", "00008140-HW")
	want := []string{"iproxy", "49200:27753", "-u", "00008140-HW"}
	if !equalArgs(args, want) {
		t.Fatalf("iproxy args = %v, want %v", args, want)
	}
}

func TestXcodegenArgs(t *testing.T) {
	args := xcodegenArgs("/c/project.yml")
	want := []string{"xcodegen", "--spec", "/c/project.yml"}
	if !equalArgs(args, want) {
		t.Fatalf("xcodegen args = %v, want %v", args, want)
	}
}

func TestTestTargetNameFromJSON(t *testing.T) {
	// A device xctestrun-as-json: one test-target dict plus the metadata entry.
	data := []byte(`{
	  "CompanionRunnerUITests": {"EnvironmentVariables": {}},
	  "__xctestrun_metadata__": {"FormatVersion": 1}
	}`)
	name, err := testTargetNameFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if name != "CompanionRunnerUITests" {
		t.Fatalf("name = %q, want CompanionRunnerUITests", name)
	}
}

func TestTestTargetNameFromJSON_IgnoresCodeCoverageInfo(t *testing.T) {
	data := []byte(`{
	  "CompanionRunnerUITests": {},
	  "CodeCoverageBuildableInfos": {},
	  "__xctestrun_metadata__": {}
	}`)
	name, err := testTargetNameFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if name != "CompanionRunnerUITests" {
		t.Fatalf("name = %q, want CompanionRunnerUITests", name)
	}
}

func TestTestTargetNameFromJSON_MultipleTargetsError(t *testing.T) {
	data := []byte(`{"A": {}, "B": {}, "__xctestrun_metadata__": {}}`)
	if _, err := testTargetNameFromJSON(data); err == nil {
		t.Fatal("multiple test-target dicts must error so the wrong dict is never patched")
	}
}

func TestReadSigningCredentialsReportsMissing(t *testing.T) {
	for _, key := range []string{envTeam, envTeamFallback, envAuthKeyPath, envAuthKeyID, envAuthIssuer} {
		t.Setenv(key, "")
	}
	_, err := readSigningCredentials()
	if err == nil {
		t.Fatal("missing credentials must error")
	}
	for _, want := range []string{envTeam, envAuthKeyPath, envAuthKeyID, envAuthIssuer} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name missing var %q: %v", want, err)
		}
	}
}

func TestReadSigningCredentialsRejectsMissingKeyFile(t *testing.T) {
	t.Setenv(envTeam, "TEAM1")
	t.Setenv(envAuthKeyID, "KID")
	t.Setenv(envAuthIssuer, "ISS")
	t.Setenv(envAuthKeyPath, filepath.Join(t.TempDir(), "absent.p8"))
	if _, err := readSigningCredentials(); err == nil {
		t.Fatal("a missing .p8 key file must error")
	}
}

func TestReadSigningCredentialsAcceptsPresentKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envTeam, "")
	t.Setenv(envTeamFallback, "FALLBACKTEAM")
	t.Setenv(envAuthKeyID, "KID")
	t.Setenv(envAuthIssuer, "ISS")
	t.Setenv(envAuthKeyPath, keyPath)
	creds, err := readSigningCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if creds.team != "FALLBACKTEAM" {
		t.Fatalf("team = %q, want the DEVELOPMENT_TEAM fallback", creds.team)
	}
}

func TestReadSigningCredentialsResolvesRelativeKeyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("AuthKey.p8", []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envTeam, "TEAM1")
	t.Setenv(envAuthKeyID, "KID")
	t.Setenv(envAuthIssuer, "ISS")
	t.Setenv(envAuthKeyPath, "AuthKey.p8")
	creds, err := readSigningCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(creds.authKeyPath) {
		t.Fatalf("authKeyPath = %q, want an absolute path for xcodebuild", creds.authKeyPath)
	}
}

func TestBuildCacheKeyChangesWithSigningIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Sources", "Server.swift"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yml"), []byte("name: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := buildCacheKey(dir, signingCredentials{team: "TEAM1", authKeyID: "KID1"})
	if err != nil {
		t.Fatal(err)
	}
	otherTeam, err := buildCacheKey(dir, signingCredentials{team: "TEAM2", authKeyID: "KID1"})
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := buildCacheKey(dir, signingCredentials{team: "TEAM1", authKeyID: "KID2"})
	if err != nil {
		t.Fatal(err)
	}
	if base == otherTeam {
		t.Fatal("a changed team must invalidate the cached build")
	}
	if base == otherKey {
		t.Fatal("a changed signing key must invalidate the cached build")
	}
}

func TestSourceHashChangesWithSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Sources", "Server.swift"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yml"), []byte("name: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sourceHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Sources", "Server.swift"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := sourceHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("a source edit must change the hash so the cached build is invalidated")
	}
}
