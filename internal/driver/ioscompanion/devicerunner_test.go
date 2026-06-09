package ioscompanion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
