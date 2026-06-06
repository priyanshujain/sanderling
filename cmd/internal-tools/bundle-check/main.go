// Command bundle-check is a developer tool that bundles a spec file to confirm it compiles.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/priyanshujain/sanderling/internal/bundler"
)

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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bundle-check <spec.ts>")
		os.Exit(1)
	}

	entryFile, err := filepath.Abs(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve spec path: %v\n", err)
		os.Exit(1)
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}

	result, err := bundleSpec(filepath.Join(repoRoot, "pkg/spec/src"), entryFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("bundled: %d bytes, sha256=%s\n", len(result.JavaScript), result.SHA256)
}
