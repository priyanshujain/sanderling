package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, name, contents string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBundle_RequiresEntryFile(t *testing.T) {
	_, err := Bundle(Options{})
	if err == nil || !strings.Contains(err.Error(), "EntryFile") {
		t.Fatalf("expected EntryFile error, got %v", err)
	}
}

func TestBundle_ProducesIIFE(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `
		const greeting: string = "hello";
		console.log(greeting);
	`)
	result, err := Bundle(Options{EntryFile: entry})
	if err != nil {
		t.Fatal(err)
	}
	body := string(result.JavaScript)
	if !strings.Contains(body, "(() =>") && !strings.Contains(body, "(function") {
		t.Errorf("expected IIFE wrapping, got: %s", body)
	}
	if !strings.Contains(body, "hello") {
		t.Errorf("expected literal to survive bundling: %s", body)
	}
	if result.SHA256 == "" {
		t.Errorf("expected non-empty SHA256")
	}
}

func TestBundle_InlinesDefines(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `
		const phone = process.env.UATU_TEST_PHONE;
		const otp = process.env.UATU_TEST_OTP;
		export const credentials = { phone, otp };
	`)
	result, err := Bundle(Options{
		EntryFile: entry,
		Defines: map[string]string{
			"UATU_TEST_PHONE": "+919876543210",
			"UATU_TEST_OTP":   "123456",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(result.JavaScript)
	if !strings.Contains(body, "+919876543210") {
		t.Errorf("phone define not inlined: %s", body)
	}
	if !strings.Contains(body, "123456") {
		t.Errorf("otp define not inlined: %s", body)
	}
}

func TestBundle_DeterministicHash(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `export const x = 1;`)
	first, err := Bundle(Options{EntryFile: entry})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Bundle(Options{EntryFile: entry})
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 {
		t.Errorf("hash should be stable: %s vs %s", first.SHA256, second.SHA256)
	}
}

func TestBundle_HashChangesWithDefines(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `
		export const phone = process.env.UATU_TEST_PHONE;
	`)
	first, err := Bundle(Options{EntryFile: entry, Defines: map[string]string{"UATU_TEST_PHONE": "1111"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Bundle(Options{EntryFile: entry, Defines: map[string]string{"UATU_TEST_PHONE": "2222"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 == second.SHA256 {
		t.Errorf("hash should change when defines change")
	}
}

func TestBundle_DefineEscapesJSONSpecialChars(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `
		export const value = process.env.WEIRD;
	`)
	result, err := Bundle(Options{
		EntryFile: entry,
		Defines:   map[string]string{"WEIRD": `quote " backslash \ newline`},
	})
	if err != nil {
		t.Fatal(err)
	}
	// esbuild may pick single or double quoting; just confirm the meaningful
	// fragments survive bundling intact.
	body := string(result.JavaScript)
	for _, fragment := range []string{`quote `, `backslash `, `newline`} {
		if !strings.Contains(body, fragment) {
			t.Errorf("missing fragment %q in bundle:\n%s", fragment, body)
		}
	}
}

func TestBundle_ImportResolution(t *testing.T) {
	directory := t.TempDir()
	helperPath := filepath.Join(directory, "helper.ts")
	entryPath := filepath.Join(directory, "entry.ts")
	if err := os.WriteFile(helperPath, []byte(`export const helperValue = 42;`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath, []byte(`
		import { helperValue } from "./helper";
		console.log(helperValue);
	`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Bundle(Options{EntryFile: entryPath})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.JavaScript), "42") {
		t.Errorf("imported value should be inlined: %s", result.JavaScript)
	}
}

func TestBundle_ReportsSyntaxErrors(t *testing.T) {
	entry := writeFixture(t, "broken.ts", `const x = ;`)
	_, err := Bundle(Options{EntryFile: entry})
	if err == nil || !strings.Contains(err.Error(), "bundle failed") {
		t.Errorf("expected bundle failure, got %v", err)
	}
}

func TestBundle_SourcemapInlinedWhenRequested(t *testing.T) {
	entry := writeFixture(t, "spec.ts", `export const x = 1;`)
	result, err := Bundle(Options{EntryFile: entry, Sourcemap: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.JavaScript), "sourceMappingURL") {
		t.Errorf("expected inline sourcemap")
	}
}
