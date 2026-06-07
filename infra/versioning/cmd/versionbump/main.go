package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mysayasan/kopiv2/infra/versioning"
)

func main() {
	manifestPath := flag.String("manifest", "infra/versioning/version.json", "version manifest path")
	pendingDir := flag.String("pending", "changes/pending", "pending changelog directory")
	appliedDir := flag.String("applied", "changes/applied", "applied changelog directory")
	commit := flag.String("commit", os.Getenv("GITHUB_SHA"), "commit SHA to store in the manifest")
	flag.Parse()

	result, err := versioning.ApplyPendingChanges(versioning.ApplyOptions{
		ManifestPath: *manifestPath,
		PendingDir:   *pendingDir,
		AppliedDir:   *appliedDir,
		Commit:       *commit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "version bump failed: %v\n", err)
		os.Exit(1)
	}
	if !result.Changed {
		fmt.Println("no pending version changes")
		return
	}

	fmt.Printf("processed %d version change(s)\n", len(result.ProcessedFiles))
	fmt.Printf("core version: %s\n", result.Manifest.Core.Version)
	for appName, entry := range result.Manifest.Apps {
		fmt.Printf("app %s version: %s\n", appName, entry.Version)
	}
}
