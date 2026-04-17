package permissions

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Inspector returns the list of uses-permission entries declared by the APK.
type Inspector func(ctx context.Context, apkPath string) ([]string, error)

// Granter grants a single permission to the given package on the connected
// device. Implementations typically wrap `adb shell pm grant`.
type Granter func(ctx context.Context, packageName, permission string) error

// GrantDangerous filters declared permissions to the dangerous set and grants
// each one via the supplied granter. Errors from individual grants are
// collected as warnings rather than aborting — Android refuses to grant
// non-runtime permissions and we prefer to soldier on.
func GrantDangerous(
	ctx context.Context,
	apkPath, packageName string,
	inspector Inspector,
	granter Granter,
) (granted []string, warnings []string, err error) {
	declared, err := inspector(ctx, apkPath)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect permissions: %w", err)
	}
	for _, permission := range declared {
		if !IsDangerous(permission) {
			continue
		}
		if err := granter(ctx, packageName, permission); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", permission, err))
			continue
		}
		granted = append(granted, permission)
	}
	return granted, warnings, nil
}

var aaptPermissionPattern = regexp.MustCompile(`uses-permission:\s+name='([^']+)'`)

// AaptInspector shells out to `aapt dump permissions` to list permissions.
func AaptInspector(ctx context.Context, apkPath string) ([]string, error) {
	output, err := exec.CommandContext(ctx, "aapt", "dump", "permissions", apkPath).Output()
	if err != nil {
		return nil, fmt.Errorf("aapt dump permissions: %w", err)
	}
	var permissions []string
	for _, match := range aaptPermissionPattern.FindAllStringSubmatch(string(output), -1) {
		permissions = append(permissions, match[1])
	}
	return permissions, nil
}

// AdbGranter returns a Granter that runs `adb shell pm grant` against the
// supplied device serial (empty = default device).
func AdbGranter(adbPath, deviceSerial string) Granter {
	return func(ctx context.Context, packageName, permission string) error {
		arguments := []string{"shell", "pm", "grant", packageName, permission}
		if deviceSerial != "" {
			arguments = append([]string{"-s", deviceSerial}, arguments...)
		}
		command := exec.CommandContext(ctx, adbPath, arguments...)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("adb pm grant %s: %w (%s)", permission, err, strings.TrimSpace(string(output)))
		}
		return nil
	}
}

// dangerousPermissions captures Android's PROTECTION_DANGEROUS set as of
// API 34. New runtime permissions added in later releases should be appended
// here when needed.
var dangerousPermissions = map[string]bool{
	"android.permission.READ_CALENDAR":                   true,
	"android.permission.WRITE_CALENDAR":                  true,
	"android.permission.CAMERA":                          true,
	"android.permission.READ_CONTACTS":                   true,
	"android.permission.WRITE_CONTACTS":                  true,
	"android.permission.GET_ACCOUNTS":                    true,
	"android.permission.ACCESS_FINE_LOCATION":            true,
	"android.permission.ACCESS_COARSE_LOCATION":          true,
	"android.permission.ACCESS_BACKGROUND_LOCATION":      true,
	"android.permission.RECORD_AUDIO":                    true,
	"android.permission.READ_PHONE_STATE":                true,
	"android.permission.READ_PHONE_NUMBERS":              true,
	"android.permission.CALL_PHONE":                      true,
	"android.permission.ANSWER_PHONE_CALLS":              true,
	"android.permission.READ_CALL_LOG":                   true,
	"android.permission.WRITE_CALL_LOG":                  true,
	"android.permission.ADD_VOICEMAIL":                   true,
	"android.permission.USE_SIP":                         true,
	"android.permission.PROCESS_OUTGOING_CALLS":          true,
	"android.permission.BODY_SENSORS":                    true,
	"android.permission.SEND_SMS":                        true,
	"android.permission.RECEIVE_SMS":                     true,
	"android.permission.READ_SMS":                        true,
	"android.permission.RECEIVE_WAP_PUSH":                true,
	"android.permission.RECEIVE_MMS":                     true,
	"android.permission.READ_EXTERNAL_STORAGE":           true,
	"android.permission.WRITE_EXTERNAL_STORAGE":          true,
	"android.permission.ACCESS_MEDIA_LOCATION":           true,
	"android.permission.ACTIVITY_RECOGNITION":            true,
	"android.permission.POST_NOTIFICATIONS":              true,
	"android.permission.NEARBY_WIFI_DEVICES":             true,
	"android.permission.READ_MEDIA_IMAGES":               true,
	"android.permission.READ_MEDIA_VIDEO":                true,
	"android.permission.READ_MEDIA_AUDIO":                true,
	"android.permission.READ_MEDIA_VISUAL_USER_SELECTED": true,
	"android.permission.BLUETOOTH_CONNECT":               true,
	"android.permission.BLUETOOTH_ADVERTISE":             true,
	"android.permission.BLUETOOTH_SCAN":                  true,
	"android.permission.UWB_RANGING":                     true,
	"android.permission.BODY_SENSORS_BACKGROUND":         true,
}

func IsDangerous(permission string) bool {
	return dangerousPermissions[permission]
}
