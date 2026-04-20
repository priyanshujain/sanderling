package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/priyanshujain/uatu/internal/bundler"
)

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
	specApiPath := filepath.Join(repoRoot, "pkg/spec-api/src/index.ts")
	defaultPropertiesPath := filepath.Join(repoRoot, "pkg/spec-api/src/defaults/properties.ts")

	result, err := bundler.Bundle(bundler.Options{
		EntryFile: entryFile,
		Aliases: map[string]string{
			"@uatu/spec":                    specApiPath,
			"@uatu/spec/defaults/properties": defaultPropertiesPath,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("bundled: %d bytes, sha256=%s\n", len(result.JavaScript), result.SHA256)
}
