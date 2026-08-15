package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion"
	"github.com/priyanshujain/sanderling/internal/ios"
	"github.com/priyanshujain/sanderling/internal/sidecarassets"
)

type doctorCheck struct {
	Name string
	Run  func(ctx context.Context) error
}

// doctorChecksFor returns the host-readiness checks for a target platform.
// "all" returns the union (deduped by name) so the legacy zero-arg `doctor`
// behaviour keeps surfacing every platform's prerequisites.
func doctorChecksFor(platform string) []doctorCheck {
	switch platform {
	case "web":
		return webChecks()
	case "android":
		return androidChecks()
	case "ios":
		return iosChecks()
	case "ios-device":
		return append(iosChecks(), iosDeviceChecks()...)
	case "all":
		return allChecks()
	default:
		return nil
	}
}

func webChecks() []doctorCheck {
	return []doctorCheck{
		{Name: "headless chromium can launch", Run: checkChromiumLaunch},
	}
}

func androidChecks() []doctorCheck {
	return []doctorCheck{
		{Name: "adb on PATH or under the Android SDK", Run: checkAdb},
		{Name: "emulator on PATH or under the Android SDK", Run: checkEmulator},
		{Name: "java 17+ on PATH", Run: checkJavaVersion},
		{Name: "sidecar JAR is real (not placeholder)", Run: checkSidecarJAR},
	}
}

// iosChecks covers the simulator path, which the native companion drives with
// no JVM. A simulator host with no Java still passes. Physical-device runs
// additionally need devicectl, the usbmuxd socket, a connected device, and
// signing credentials, covered by iosDeviceChecks and surfaced through the
// "all" union.
func iosChecks() []doctorCheck {
	return []doctorCheck{
		{Name: "xcrun on PATH (ios simulator)", Run: checkExecutableOnPath("xcrun")},
		{Name: "simctl available (ios simulator)", Run: checkSimctl},
	}
}

// iosDeviceChecks covers the prerequisites a physical iOS device needs: the
// runner is built and driven over a native usbmux tunnel, so devicectl installs
// the app, the macOS usbmuxd socket carries the tunnel, a device must be
// connected and paired, and App Store Connect signing credentials must be
// present for the no-UI build. Everything here is part of macOS + Xcode; nothing
// is installed.
func iosDeviceChecks() []doctorCheck {
	return []doctorCheck{
		{Name: "devicectl available (ios physical device)", Run: checkDevicectl},
		{Name: "usbmuxd socket present (ios physical device)", Run: checkUsbmuxd},
		{Name: "an iOS device is connected and paired", Run: checkDeviceConnected},
		{Name: "App Store Connect signing credentials present", Run: checkDeviceSigning},
	}
}

func allChecks() []doctorCheck {
	seen := map[string]bool{}
	var combined []doctorCheck
	for _, group := range [][]doctorCheck{webChecks(), androidChecks(), iosChecks(), iosDeviceChecks()} {
		for _, c := range group {
			if seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			combined = append(combined, c)
		}
	}
	return combined
}

// checkChromiumLaunch boots a headless chromium under chromedp's default
// allocator, opens a blank tab, and tears down. Confirms the bundled CDP
// surface plus a working Chromium binary path.
func checkChromiumLaunch(ctx context.Context) error {
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
		)...,
	)
	defer allocCancel()
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	defer tabCancel()
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		return fmt.Errorf("chromium launch: %w", err)
	}
	return nil
}

// checkSimctl exercises `xcrun simctl help`: simctl is an xcrun subcommand,
// not a standalone binary, so a PATH lookup can never find it.
func checkSimctl(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "xcrun", "simctl", "help").Run(); err != nil {
		return fmt.Errorf("xcrun simctl help: %w", err)
	}
	return nil
}

// Device-check seams: package-level so the doctor's device checks run against
// canned results instead of a real device.
var (
	doctorConnectedDevices = ios.ConnectedDevices
	doctorVerifySigning    = ioscompanion.VerifyDeviceSigning
	doctorVerifyUsbmuxd    = ioscompanion.VerifyUsbmuxdSocket
)

// checkDevicectl exercises `xcrun devicectl --version`: devicectl is an xcrun
// subcommand, so a PATH lookup cannot find it.
func checkDevicectl(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "xcrun", "devicectl", "--version").Run(); err != nil {
		return fmt.Errorf("xcrun devicectl --version: %w", err)
	}
	return nil
}

// checkUsbmuxd confirms the macOS usbmuxd socket is present: the native device
// tunnel speaks to it directly instead of shelling out to a third-party client.
func checkUsbmuxd(_ context.Context) error {
	return doctorVerifyUsbmuxd()
}

// checkDeviceConnected confirms at least one physical iOS device is connected
// and paired, the prerequisite for the tunnel and the install.
func checkDeviceConnected(ctx context.Context) error {
	devices, err := doctorConnectedDevices(ctx)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("no connected iOS device; connect and pair an iPhone")
	}
	return nil
}

// checkDeviceSigning confirms the App Store Connect signing environment is
// complete and the key file exists, so the no-UI device build can sign.
func checkDeviceSigning(_ context.Context) error {
	return doctorVerifySigning()
}

func checkSidecarJAR(_ context.Context) error {
	if sidecarassets.IsPlaceholder() {
		return fmt.Errorf("placeholder JAR embedded; run `make sidecar && make sanderling` to embed the real fat JAR")
	}
	if sidecarassets.EmbeddedSize() == 0 {
		return fmt.Errorf("embedded JAR is empty")
	}
	return nil
}

type doctorOptions struct {
	platform string
}

func parseDoctorArgs(args []string, stderr io.Writer) (doctorOptions, error) {
	flagSet := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	var options doctorOptions
	flagSet.StringVar(&options.platform, "platform", "all", "target platform: web, android, ios, ios-device, all")
	if err := flagSet.Parse(args); err != nil {
		return doctorOptions{}, err
	}
	switch options.platform {
	case "web", "android", "ios", "ios-device", "all":
		return options, nil
	default:
		return doctorOptions{}, fmt.Errorf("unsupported platform: %q (web, android, ios, ios-device, all)", options.platform)
	}
}

// doctorCheckTimeout bounds a single host-readiness check. Most checks (exec
// lookups, file stats, java -version) finish in milliseconds, but
// checkChromiumLaunch boots a real browser and can exceed 5s on a cold CI
// host - 15s leaves headroom without making real failures feel hung.
const doctorCheckTimeout = 15 * time.Second

func runDoctorChecks(ctx context.Context, checks []doctorCheck, stdout io.Writer) error {
	failures := 0
	for _, check := range checks {
		callCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
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

// checkAdb and checkEmulator resolve through the same helpers a run uses, so a
// host the doctor passes is a host a run can drive.
func checkAdb(_ context.Context) error {
	_, err := android.AdbBinary()
	return err
}

func checkEmulator(_ context.Context) error {
	_, err := android.EmulatorBinary()
	return err
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
