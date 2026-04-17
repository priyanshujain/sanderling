package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type Options struct {
	EntryFile string
	Defines   map[string]string
	Aliases   map[string]string
	Sourcemap bool
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

	output := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{options.EntryFile},
		Bundle:      true,
		Format:      esbuild.FormatIIFE,
		Target:      esbuild.ES2020,
		Platform:    esbuild.PlatformNeutral,
		Define:      defines,
		Alias:       options.Aliases,
		Sourcemap:   sourcemap,
		Write:       false,
		LogLevel:    esbuild.LogLevelSilent,
	})

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
