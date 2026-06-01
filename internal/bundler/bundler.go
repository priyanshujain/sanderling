// Package bundler compiles a TypeScript spec into a single JavaScript bundle via esbuild.
package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type Options struct {
	EntryFile string
	// RuntimeFile, when set, is imported BEFORE the spec via a stdin entry so
	// the bundle installs __sanderlingNextAction__ / __sanderlingExtractors__
	// (the goja runtime entry wires the shared picker). Empty bundles the spec
	// alone, as the bundle-check tool and unit fixtures do.
	RuntimeFile string
	Defines     map[string]string
	Aliases     map[string]string
	Sourcemap   bool
}

type Result struct {
	JavaScript []byte
	SHA256     string
}

// Bundle compiles the entry TypeScript file into a single IIFE JavaScript
// blob targeting ES2020. Defines are injected as literal string values for
// process.env.<NAME> lookups in the spec source.
func Bundle(options Options) (Result, error) {
	if options.EntryFile == "" {
		return Result{}, errors.New("EntryFile is required")
	}

	defines := map[string]string{}
	for key, value := range options.Defines {
		quoted, err := json.Marshal(value)
		if err != nil {
			return Result{}, fmt.Errorf("define %q: %w", key, err)
		}
		defines["process.env."+key] = string(quoted)
	}

	sourcemap := esbuild.SourceMapNone
	if options.Sourcemap {
		sourcemap = esbuild.SourceMapInline
	}

	buildOptions := esbuild.BuildOptions{
		Bundle:    true,
		Format:    esbuild.FormatIIFE,
		Target:    esbuild.ES2020,
		Platform:  esbuild.PlatformNeutral,
		Define:    defines,
		Alias:     options.Aliases,
		Sourcemap: sourcemap,
		Write:     false,
		LogLevel:  esbuild.LogLevelSilent,
	}
	if options.RuntimeFile == "" {
		buildOptions.EntryPoints = []string{options.EntryFile}
	} else {
		runtimeAbs, err := filepath.Abs(options.RuntimeFile)
		if err != nil {
			return Result{}, fmt.Errorf("runtime path: %w", err)
		}
		specAbs, err := filepath.Abs(options.EntryFile)
		if err != nil {
			return Result{}, fmt.Errorf("entry path: %w", err)
		}
		buildOptions.Stdin = &esbuild.StdinOptions{
			Contents:   fmt.Sprintf("import %q;\n%s", runtimeAbs, registrationEntry(specAbs)),
			ResolveDir: filepath.Dir(specAbs),
			Loader:     esbuild.LoaderTS,
		}
	}

	output := esbuild.Build(buildOptions)

	if len(output.Errors) > 0 {
		var messages []string
		for _, err := range output.Errors {
			messages = append(messages, err.Text)
		}
		return Result{}, fmt.Errorf("bundle failed: %s", strings.Join(messages, "; "))
	}
	if len(output.OutputFiles) == 0 {
		return Result{}, errors.New("bundle produced no output files")
	}

	javascript := output.OutputFiles[0].Contents
	sum := sha256.Sum256(javascript)
	return Result{
		JavaScript: javascript,
		SHA256:     hex.EncodeToString(sum[:]),
	}, nil
}

// registrationEntry imports the spec as a namespace and copies its named
// exports onto globalThis so authors write plain `export const properties /
// actionsRoot / setup` without hand-assigning globalThis. The guards let web
// specs omit setup. esbuild keeps the exports because the trailer references
// them; output stays an IIFE.
func registrationEntry(specAbs string) string {
	return fmt.Sprintf(`import * as __spec from %q;
if (__spec.actionsRoot !== undefined) globalThis.actions = __spec.actionsRoot;
if (__spec.properties !== undefined) globalThis.properties = __spec.properties;
if (__spec.setup !== undefined) globalThis.setup = __spec.setup;
`, specAbs)
}
