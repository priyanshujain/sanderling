package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

// WebOptions configures BundleWeb. WebRuntimeFile points at
// pkg/spec/src/web-runtime.ts (resolved via the same upward search the regular
// Bundle uses for @sanderling/spec). When empty, BundleWeb expects callers to
// have set Aliases such that "@sanderling/spec/web-runtime" resolves.
type WebOptions struct {
	EntryFile      string
	WebRuntimeFile string
	Defines        map[string]string
	Aliases        map[string]string
}

// BundleWeb compiles the user spec together with the V8-side runtime into a
// single IIFE that, on evaluation, installs `globalThis.__sanderling__` and
// the `__sanderlingExtractors__` / `__sanderlingNextAction__` globals. The
// host injects the result via Page.AddScriptToEvaluateOnNewDocument.
func BundleWeb(options WebOptions) (Result, error) {
	if options.EntryFile == "" {
		return Result{}, errors.New("EntryFile is required")
	}
	if options.WebRuntimeFile == "" {
		return Result{}, errors.New("WebRuntimeFile is required")
	}
	if _, err := filepath.Abs(options.EntryFile); err != nil {
		return Result{}, fmt.Errorf("entry path: %w", err)
	}
	runtimeAbs, err := filepath.Abs(options.WebRuntimeFile)
	if err != nil {
		return Result{}, fmt.Errorf("runtime path: %w", err)
	}
	specAbs, err := filepath.Abs(options.EntryFile)
	if err != nil {
		return Result{}, fmt.Errorf("entry path: %w", err)
	}

	defines := map[string]string{}
	for key, value := range options.Defines {
		defines["process.env."+key] = quoteJSString(value)
	}

	stdinContents := fmt.Sprintf(`import %q;
import %q;
`, runtimeAbs, specAbs)

	output := esbuild.Build(esbuild.BuildOptions{
		Stdin: &esbuild.StdinOptions{
			Contents:   stdinContents,
			ResolveDir: filepath.Dir(specAbs),
			Loader:     esbuild.LoaderTS,
		},
		Bundle:   true,
		Format:   esbuild.FormatIIFE,
		Target:   esbuild.ES2020,
		Platform: esbuild.PlatformBrowser,
		Define:   defines,
		Alias:    options.Aliases,
		Write:    false,
		LogLevel: esbuild.LogLevelSilent,
	})

	if len(output.Errors) > 0 {
		var messages []string
		for _, message := range output.Errors {
			messages = append(messages, message.Text)
		}
		return Result{}, fmt.Errorf("web bundle failed: %s", strings.Join(messages, "; "))
	}
	if len(output.OutputFiles) == 0 {
		return Result{}, errors.New("web bundle produced no output files")
	}

	javascript := output.OutputFiles[0].Contents
	sum := sha256.Sum256(javascript)
	return Result{
		JavaScript: javascript,
		SHA256:     hex.EncodeToString(sum[:]),
	}, nil
}

func quoteJSString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for index := 0; index < len(value); index++ {
		c := value[index]
		switch c {
		case '"', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(c)
		case '\n':
			builder.WriteString("\\n")
		case '\r':
			builder.WriteString("\\r")
		case '\t':
			builder.WriteString("\\t")
		default:
			builder.WriteByte(c)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}
