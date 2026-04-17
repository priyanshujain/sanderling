package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/priyanshujain/uatu/internal/bundler"
)

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}
	specApiPath := filepath.Join(repoRoot, "pkg/spec-api/src/index.ts")

	result, err := bundler.Bundle(bundler.Options{
		EntryFile: "examples/specs/merchant-ledger.ts",
		Defines: map[string]string{
			"UATU_TEST_PHONE": "+910000000000",
			"UATU_TEST_OTP":   "000000",
		},
		Aliases: map[string]string{
			"@uatu/spec": specApiPath,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("bundled: %d bytes, sha256=%s\n", len(result.JavaScript), result.SHA256)
}
