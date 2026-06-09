package ioscompanion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// developmentTeam is read from the environment so the account-specific team id
// is never committed. The App Store Connect API key path/id/issuer come from the
// same source. These back the no-Xcode-UI signing path.
const (
	envTeam         = "SANDERLING_IOS_TEAM"
	envTeamFallback = "DEVELOPMENT_TEAM"
	envAuthKeyPath  = "ASC_API_KEY_PATH"
	envAuthKeyID    = "ASC_API_KEY_ID"
	envAuthIssuer   = "ASC_API_ISSUER_ID"
	envCompanionDir = "SANDERLING_COMPANION_DIR"
)

// deviceRunnerScheme and deviceRunnerTarget mirror companion/project.yml. The
// build product is the runner whose xctestrun the test session consumes.
const (
	deviceRunnerScheme  = "CompanionRunner"
	deviceRunnerProject = "CompanionRunner.xcodeproj"
)

// signingCredentials carries the no-UI signing inputs read from the environment.
type signingCredentials struct {
	team         string
	authKeyPath  string
	authKeyID    string
	authIssuerID string
}

// readSigningCredentials gathers the signing inputs from the environment and
// reports every missing one at once. The .p8 key must exist on disk.
func readSigningCredentials() (signingCredentials, error) {
	creds := signingCredentials{
		team:         firstNonEmpty(os.Getenv(envTeam), os.Getenv(envTeamFallback)),
		authKeyPath:  os.Getenv(envAuthKeyPath),
		authKeyID:    os.Getenv(envAuthKeyID),
		authIssuerID: os.Getenv(envAuthIssuer),
	}
	var missing []string
	if creds.team == "" {
		missing = append(missing, envTeam)
	}
	if creds.authKeyPath == "" {
		missing = append(missing, envAuthKeyPath)
	}
	if creds.authKeyID == "" {
		missing = append(missing, envAuthKeyID)
	}
	if creds.authIssuerID == "" {
		missing = append(missing, envAuthIssuer)
	}
	if len(missing) > 0 {
		return creds, fmt.Errorf("device signing requires environment variables: %s", strings.Join(missing, ", "))
	}
	// xcodebuild's -authenticationKeyPath demands an absolute path, but .env
	// files commonly carry a repo-relative one. Resolve it against the working
	// directory before the stat so a relative key still works.
	if absolute, err := filepath.Abs(creds.authKeyPath); err == nil {
		creds.authKeyPath = absolute
	}
	if _, err := os.Stat(creds.authKeyPath); err != nil {
		return creds, fmt.Errorf("App Store Connect key not found at %s: %w", creds.authKeyPath, err)
	}
	return creds, nil
}

// VerifyDeviceSigning reports whether the device signing environment is complete
// and the App Store Connect key file exists. The doctor calls it so the device
// preflight surfaces missing credentials before a run reaches the build step.
func VerifyDeviceSigning() error {
	_, err := readSigningCredentials()
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// realSpawnDeviceRunner regenerates the runner project, builds it for the device
// (skipping when the cached build matches the current sources), injects the
// session port into the device xctestrun, and spawns the test session that hosts
// the runner on the device. address carries the host loopback port, reused as
// the device-side COMPANION_PORT.
func (d *Driver) realSpawnDeviceRunner(ctx context.Context, address string) (*exec.Cmd, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	companionDir, err := resolveCompanionDir()
	if err != nil {
		return nil, err
	}
	creds, err := readSigningCredentials()
	if err != nil {
		return nil, err
	}

	derivedDataPath := filepath.Join(os.TempDir(), "sanderling-device-runner")
	if err := os.MkdirAll(derivedDataPath, 0o755); err != nil {
		return nil, err
	}

	if err := d.buildDeviceRunnerIfNeeded(ctx, companionDir, derivedDataPath, creds); err != nil {
		return nil, err
	}

	xctestrunPath, err := locateDeviceXctestrun(derivedDataPath)
	if err != nil {
		return nil, err
	}
	if err := injectCompanionPort(ctx, xctestrunPath, port); err != nil {
		return nil, fmt.Errorf("inject companion port: %w", err)
	}

	logPath := filepath.Join(derivedDataPath, "device-session-"+port+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create device session log: %w", err)
	}

	args := testWithoutBuildingArgs(xctestrunPath, d.udid, creds)
	command := exec.CommandContext(ctx, "xcrun", args...)
	command.Stdout = logFile
	command.Stderr = logFile
	// Minimal environment: the session can echo its environment into the run
	// log, so secrets in the parent environment must not reach it. Signing is
	// passed by flag (a key-file path), not env.
	command.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=/usr/bin:/bin",
		"TMPDIR=" + os.TempDir(),
	}
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = shutdownGrace
	startErr := command.Start()
	logFile.Close()
	if startErr != nil {
		return nil, fmt.Errorf("start device session: %w", startErr)
	}
	fmt.Fprintf(d.output, "device runner session pid=%d port=%s (log: %s)\n", command.Process.Pid, port, logPath)
	return command, nil
}

// buildDeviceRunnerIfNeeded regenerates the project and runs build-for-testing,
// skipping the build when a marker recording the current build key already
// matches. The device signature is per-account/per-device, so the build cannot
// be embedded; the stable derivedDataPath makes the build incremental.
func (d *Driver) buildDeviceRunnerIfNeeded(ctx context.Context, companionDir, derivedDataPath string, creds signingCredentials) error {
	key, err := buildCacheKey(companionDir, creds)
	if err != nil {
		return err
	}
	marker := filepath.Join(derivedDataPath, "device-runner.sha256")
	if existing, readErr := os.ReadFile(marker); readErr == nil && string(existing) == key {
		fmt.Fprintln(d.output, "device runner build is up to date; skipping build")
		return nil
	}

	if out, genErr := runQuiet(ctx, companionDir, xcodegenArgs(filepath.Join(companionDir, "project.yml"))...); genErr != nil {
		return fmt.Errorf("xcodegen: %w: %s", genErr, strings.TrimSpace(string(out)))
	}

	projectPath := filepath.Join(companionDir, deviceRunnerProject)
	args := buildForTestingArgs(projectPath, derivedDataPath, creds)
	fmt.Fprintln(d.output, "building device runner (first run is slow; subsequent runs are cached)")
	if out, buildErr := runQuiet(ctx, companionDir, append([]string{"xcrun"}, args...)...); buildErr != nil {
		return fmt.Errorf("build-for-testing: %w: %s", buildErr, tailLines(string(out), 20))
	}
	if err := os.WriteFile(marker, []byte(key), 0o644); err != nil {
		return err
	}
	return nil
}

// buildCacheKey combines the source hash with the signing identity so a changed
// team or key invalidates the cached build. A runner signed with a stale
// identity would otherwise be reused and rejected at install (0xe8008018).
func buildCacheKey(companionDir string, creds signingCredentials) (string, error) {
	sources, err := sourceHash(companionDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sources + "\x00" + creds.team + "\x00" + creds.authKeyID))
	return hex.EncodeToString(sum[:]), nil
}

// xcodegenArgs regenerates the runner project from its spec.
func xcodegenArgs(specPath string) []string {
	return []string{"xcodegen", "--spec", specPath}
}

// buildForTestingArgs builds the runner for a generic device destination, signed
// through the App Store Connect API key with automatic provisioning. A generic
// destination keeps the build off any specific booted device; the wildcard dev
// profile covers every provisioned device in the team.
func buildForTestingArgs(projectPath, derivedDataPath string, creds signingCredentials) []string {
	return []string{"xcodebuild", "build-for-testing",
		"-project", projectPath,
		"-scheme", deviceRunnerScheme,
		"-destination", "generic/platform=iOS",
		"-derivedDataPath", derivedDataPath,
		"-allowProvisioningUpdates",
		"-authenticationKeyPath", creds.authKeyPath,
		"-authenticationKeyID", creds.authKeyID,
		"-authenticationKeyIssuerID", creds.authIssuerID,
		// companion/project.yml disables signing for the simulator build; the
		// device install rejects an unsigned runner (0xe8008018), so signing is
		// re-enabled here and the team drives automatic provisioning.
		"CODE_SIGNING_ALLOWED=YES",
		"CODE_SIGNING_REQUIRED=YES",
		"CODE_SIGN_STYLE=Automatic",
		"DEVELOPMENT_TEAM=" + creds.team,
		"GENERATE_INFOPLIST_FILE=YES",
	}
}

// testWithoutBuildingArgs runs the prebuilt runner's test session on the
// specific device, installing the signed runner via the same automatic
// provisioning the build used.
func testWithoutBuildingArgs(xctestrunPath, hardwareUDID string, creds signingCredentials) []string {
	return []string{"xcodebuild", "test-without-building",
		"-xctestrun", xctestrunPath,
		"-destination", "platform=iOS,id=" + hardwareUDID,
		"-allowProvisioningUpdates",
		"-authenticationKeyPath", creds.authKeyPath,
		"-authenticationKeyID", creds.authKeyID,
		"-authenticationKeyIssuerID", creds.authIssuerID,
	}
}

// locateDeviceXctestrun finds the device build's xctestrun under the derived
// data products. The name embeds the device SDK version, so it is discovered
// rather than hardcoded.
func locateDeviceXctestrun(derivedDataPath string) (string, error) {
	products := filepath.Join(derivedDataPath, "Build", "Products")
	entries, err := os.ReadDir(products)
	if err != nil {
		return "", fmt.Errorf("read build products: %w", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".xctestrun") {
			return filepath.Join(products, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("no xctestrun under %s", products)
}

// injectCompanionPort sets COMPANION_PORT in the runner's environment inside the
// device xctestrun. The device xctestrun has no port placeholder and its
// test-target dict name embeds the SDK, so the name is parsed from the plist and
// the key is set (created when absent).
func injectCompanionPort(ctx context.Context, xctestrunPath, port string) error {
	jsonBytes, err := plistAsJSON(ctx, xctestrunPath)
	if err != nil {
		return err
	}
	target, err := testTargetNameFromJSON(jsonBytes)
	if err != nil {
		return err
	}
	keyPath := ":" + target + ":EnvironmentVariables:COMPANION_PORT"
	// Set updates an existing key; when the key is absent (the common device
	// case) Set fails and Add creates it. Running both, ignoring Set's failure,
	// makes the injection idempotent across reruns against a cached build.
	_ = exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Set "+keyPath+" "+port, xctestrunPath).Run()
	if out, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy",
		"-c", "Add "+keyPath+" string "+port, xctestrunPath).CombinedOutput(); err != nil {
		// Add fails when the key already exists, which means Set above succeeded.
		if !strings.Contains(string(out), "Entry Already Exists") {
			return fmt.Errorf("PlistBuddy: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// plistAsJSON converts a plist to JSON via plutil so it can be parsed without a
// plist library.
func plistAsJSON(ctx context.Context, plistPath string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", plistPath).Output()
	if err != nil {
		return nil, fmt.Errorf("plutil convert %s: %w", plistPath, err)
	}
	return out, nil
}

// testTargetNameFromJSON returns the single test-target dict name in a parsed
// xctestrun: the lone top-level key that is not the metadata entry.
func testTargetNameFromJSON(data []byte) (string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return "", fmt.Errorf("parse xctestrun json: %w", err)
	}
	var names []string
	for key := range top {
		if key == "__xctestrun_metadata__" || strings.HasPrefix(key, "CodeCoverageBuildableInfos") {
			continue
		}
		names = append(names, key)
	}
	sort.Strings(names)
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", fmt.Errorf("xctestrun has no test-target dict")
	default:
		return "", fmt.Errorf("xctestrun has multiple test-target dicts: %s", strings.Join(names, ", "))
	}
}

// sourceHash digests the runner sources and project spec so a source edit
// invalidates the cached device build. Mirrors the runnerassets checksum reuse.
func sourceHash(companionDir string) (string, error) {
	var paths []string
	sourcesDir := filepath.Join(companionDir, "Sources")
	walkErr := filepath.Walk(sourcesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	paths = append(paths, filepath.Join(companionDir, "project.yml"))
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\n", path)
		hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// resolveCompanionDir finds the companion source tree: the SANDERLING_COMPANION_DIR
// override if set, otherwise the nearest ancestor of the working directory that
// holds companion/project.yml. The device runner is built from source at run
// time, so the tree must be present (it is, in a source checkout).
func resolveCompanionDir() (string, error) {
	if override := os.Getenv(envCompanionDir); override != "" {
		if _, err := os.Stat(filepath.Join(override, "project.yml")); err != nil {
			return "", fmt.Errorf("%s=%s has no project.yml: %w", envCompanionDir, override, err)
		}
		return override, nil
	}
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(directory, "companion")
		if _, statErr := os.Stat(filepath.Join(candidate, "project.yml")); statErr == nil {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return "", fmt.Errorf("companion source tree not found; run from a sanderling checkout or set %s", envCompanionDir)
}

// runQuiet runs a command in dir with the inherited environment and returns its
// combined output. Used for the transient build steps (xcodegen, xcodebuild
// build-for-testing) whose output is surfaced only on failure.
func runQuiet(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = dir
	return command.CombinedOutput()
}

// tailLines returns the last n lines of text, so a long xcodebuild failure log
// surfaces its tail (where the error is) without flooding the run output.
func tailLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
