package main

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseInstallationContract(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Run("versioned module installation", func(t *testing.T) {
		if !strings.HasPrefix(read(t, "go.mod"), "module github.com/Hacerx/sf-typings\n") {
			t.Fatal("module path must support the documented go install command")
		}
		if strings.TrimSpace(read(t, "VERSION")) != "1.0.0" {
			t.Fatal("initial release must be 1.0.0")
		}
	})
	t.Run("manual release targets", func(t *testing.T) {
		workflow := read(t, ".github/workflows/release.yml")
		for _, required := range []string{"workflow_dispatch:", "goos: [linux, windows, darwin]", "goarch: [amd64, arm64]", "contents: write", "sha256sum", "gh release create", "needs: build"} {
			if !strings.Contains(workflow, required) {
				t.Errorf("release workflow missing %q", required)
			}
		}
	})
	t.Run("installation documentation", func(t *testing.T) {
		doc := read(t, "README.md")
		for _, required := range []string{"## Install", "go install github.com/Hacerx/sf-typings@v1.0.0", "SHA256SUMS", "releases/tag/v1.0.0", "Actions", "Run workflow"} {
			if !strings.Contains(doc, required) {
				t.Errorf("installation documentation missing %q", required)
			}
		}
	})
}
