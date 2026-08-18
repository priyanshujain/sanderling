// Command bundle-check is a developer tool that bundles a spec file and loads
// it into the evaluator to confirm it compiles and registers properties.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/testrun"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// checkSeed keeps the load deterministic. The bundle it seeds is only loaded,
// never hashed or reported, so the value is arbitrary.
const checkSeed = 1

func bundleSpec(specSrc, entryFile string) (bundler.Result, error) {
	return bundler.Bundle(bundler.Options{
		EntryFile: entryFile,
		Aliases: map[string]string{
			"@sanderling/spec":                     filepath.Join(specSrc, "index.ts"),
			"@sanderling/spec/defaults":            filepath.Join(specSrc, "defaults/index.ts"),
			"@sanderling/spec/defaults/properties": filepath.Join(specSrc, "defaults/properties.ts"),
		},
	})
}

// registeredProperties bundles the spec the way a run bundles it, with the
// runtime entry that assigns globalThis.properties, and loads it into the real
// evaluator. Bundling alone proves nothing about registration: a spec that
// registers no property compiles perfectly and then judges nothing.
func registeredProperties(entryFile string) ([]string, error) {
	bundle, err := testrun.BundleSpec(entryFile, checkSeed)
	if err != nil {
		return nil, fmt.Errorf("bundle with runtime entry: %w", err)
	}
	evaluator, err := verifier.New()
	if err != nil {
		return nil, fmt.Errorf("evaluator: %w", err)
	}
	if err := evaluator.Load(string(bundle.JavaScript)); err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	return evaluator.PropertyNames(), nil
}

func check(specSrc, entryFile string, stdout io.Writer) error {
	return checkWithOptions(specSrc, entryFile, false, stdout)
}

func checkWithOptions(specSrc, entryFile string, allowNoProperties bool, stdout io.Writer) error {
	result, err := bundleSpec(specSrc, entryFile)
	if err != nil {
		return fmt.Errorf("bundle: %w", err)
	}
	fmt.Fprintf(stdout, "bundled: %d bytes, sha256=%s\n", len(result.JavaScript), result.SHA256)

	names, err := registeredProperties(entryFile)
	if err != nil {
		return err
	}
	if len(names) == 0 && !allowNoProperties {
		return errors.New("the spec bundles and loads cleanly but registers no properties: " +
			"nothing is wrong with the source, and a run against it would check nothing " +
			"and report no violations. Pass --allow-no-properties for a spec that measures " +
			"what it extracts or where the generator reaches")
	}
	fmt.Fprintf(stdout, "properties registered: %d (%s)\n", len(names), strings.Join(names, ", "))
	return nil
}

func main() {
	flagSet := flag.NewFlagSet("bundle-check", flag.ExitOnError)
	allowNoProperties := flagSet.Bool("allow-no-properties", false,
		"accept a spec that registers no properties, for a pre-registration that measures what the spec extracts or where the generator reaches")
	flagSet.Usage = func() {
		fmt.Fprintln(flagSet.Output(), "usage: bundle-check [--allow-no-properties] <spec.ts>")
		flagSet.PrintDefaults()
	}
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	if flagSet.NArg() != 1 {
		flagSet.Usage()
		os.Exit(1)
	}

	entryFile, err := filepath.Abs(flagSet.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve spec path: %v\n", err)
		os.Exit(1)
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}

	if err := checkWithOptions(filepath.Join(repoRoot, "pkg/spec/src"), entryFile, *allowNoProperties, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
