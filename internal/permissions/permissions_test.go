package permissions

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGrantDangerous_FiltersAndCallsGranter(t *testing.T) {
	declared := []string{
		"android.permission.INTERNET",                  // not dangerous
		"android.permission.CAMERA",                    // dangerous
		"android.permission.WAKE_LOCK",                 // not dangerous
		"android.permission.ACCESS_FINE_LOCATION",      // dangerous
		"android.permission.READ_EXTERNAL_STORAGE",     // dangerous
		"android.permission.SYSTEM_ALERT_WINDOW",       // not dangerous
	}
	inspector := func(_ context.Context, _ string) ([]string, error) { return declared, nil }
	var requested []string
	granter := func(_ context.Context, packageName, permission string) error {
		if packageName != "com.example" {
			t.Errorf("granter received wrong package: %q", packageName)
		}
		requested = append(requested, permission)
		return nil
	}

	granted, warnings, err := GrantDangerous(context.Background(), "/path/to/apk", "com.example", inspector, granter)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"android.permission.CAMERA",
		"android.permission.ACCESS_FINE_LOCATION",
		"android.permission.READ_EXTERNAL_STORAGE",
	}
	if !slices.Equal(granted, want) {
		t.Errorf("granted permissions: got %v, want %v", granted, want)
	}
	if !slices.Equal(requested, want) {
		t.Errorf("granter calls: got %v, want %v", requested, want)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestGrantDangerous_CollectsGranterFailuresAsWarnings(t *testing.T) {
	declared := []string{"android.permission.CAMERA", "android.permission.RECORD_AUDIO"}
	inspector := func(_ context.Context, _ string) ([]string, error) { return declared, nil }
	granter := func(_ context.Context, _, permission string) error {
		if permission == "android.permission.CAMERA" {
			return errors.New("device denied")
		}
		return nil
	}

	granted, warnings, err := GrantDangerous(context.Background(), "/x", "com.example", inspector, granter)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(granted, []string{"android.permission.RECORD_AUDIO"}) {
		t.Errorf("granted: %v", granted)
	}
	if len(warnings) != 1 || warnings[0] != "android.permission.CAMERA: device denied" {
		t.Errorf("warnings: %v", warnings)
	}
}

func TestGrantDangerous_InspectorErrorBubbles(t *testing.T) {
	inspector := func(_ context.Context, _ string) ([]string, error) {
		return nil, errors.New("aapt missing")
	}
	_, _, err := GrantDangerous(context.Background(), "/x", "com.example", inspector, nil)
	if err == nil || err.Error() == "" {
		t.Errorf("expected wrapped inspector error, got %v", err)
	}
}

func TestIsDangerous_KnownPermissions(t *testing.T) {
	dangerous := []string{
		"android.permission.CAMERA",
		"android.permission.RECORD_AUDIO",
		"android.permission.POST_NOTIFICATIONS",
	}
	for _, permission := range dangerous {
		if !IsDangerous(permission) {
			t.Errorf("expected %q to be dangerous", permission)
		}
	}

	normal := []string{
		"android.permission.INTERNET",
		"android.permission.WAKE_LOCK",
		"android.permission.SYSTEM_ALERT_WINDOW",
	}
	for _, permission := range normal {
		if IsDangerous(permission) {
			t.Errorf("expected %q not to be dangerous", permission)
		}
	}
}

func TestAaptInspector_ParsesFixtureOutput(t *testing.T) {
	// Direct test of the regex; avoids spawning aapt.
	output := `
package: com.example
sdkVersion:'24'
uses-permission: name='android.permission.INTERNET'
uses-permission: name='android.permission.CAMERA'
uses-permission: name='android.permission.WAKE_LOCK'
`
	matches := aaptPermissionPattern.FindAllStringSubmatch(output, -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"android.permission.INTERNET",
		"android.permission.CAMERA",
		"android.permission.WAKE_LOCK",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
