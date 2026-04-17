package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

type doctorCheck struct {
	Name string
	Run  func(ctx context.Context) error
}

func defaultDoctorChecks() []doctorCheck {
	return []doctorCheck{
		{Name: "adb on PATH", Run: checkExecutableOnPath("adb")},
		{Name: "emulator on PATH or under ANDROID_HOME", Run: checkEmulator},
		{Name: "java 17+ on PATH", Run: checkJavaVersion},
	}
}

func runDoctorChecks(ctx context.Context, checks []doctorCheck, stdout io.Writer) error {
	failures := 0
	for _, check := range checks {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := check.Run(callCtx)
		cancel()
		if err != nil {
			fmt.Fprintf(stdout, "FAIL  %s: %v\n", check.Name, err)
			failures++
			continue
		}
		fmt.Fprintf(stdout, "OK    %s\n", check.Name)
	}
	if failures > 0 {
		return fmt.Errorf("%d check(s) failed", failures)
	}
	return nil
}

func checkExecutableOnPath(name string) func(context.Context) error {
	return func(_ context.Context) error {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("not found: %w", err)
		}
		return nil
	}
}

func checkEmulator(_ context.Context) error {
	if _, err := exec.LookPath("emulator"); err == nil {
		return nil
	}
	androidHome := os.Getenv("ANDROID_HOME")
	if androidHome == "" {
		androidHome = os.Getenv("ANDROID_SDK_ROOT")
	}
	if androidHome == "" {
		return fmt.Errorf("not on PATH and ANDROID_HOME is unset")
	}
	candidate := filepath.Join(androidHome, "emulator", "emulator")
	if _, err := os.Stat(candidate); err != nil {
		return fmt.Errorf("not found at %s", candidate)
	}
	return nil
}

var javaVersionPattern = regexp.MustCompile(`(?:java|openjdk)[^"]*"(\d+)(?:\.(\d+))?`)

func checkJavaVersion(ctx context.Context) error {
	if _, err := exec.LookPath("java"); err != nil {
		return fmt.Errorf("java not found: %w", err)
	}
	output, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("java -version: %w", err)
	}
	major, err := parseJavaMajor(string(output))
	if err != nil {
		return err
	}
	if major < 17 {
		return fmt.Errorf("java major version %d is less than 17", major)
	}
	return nil
}

func parseJavaMajor(versionOutput string) (int, error) {
	match := javaVersionPattern.FindStringSubmatch(versionOutput)
	if match == nil {
		return 0, fmt.Errorf("could not parse java version from %q", firstLine(versionOutput))
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("non-numeric major %q", match[1])
	}
	if major == 1 && len(match) >= 3 && match[2] != "" {
		minor, err := strconv.Atoi(match[2])
		if err == nil {
			return minor, nil
		}
	}
	return major, nil
}

func firstLine(text string) string {
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			return text[:index]
		}
	}
	return text
}
