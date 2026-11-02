package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Hacerx/sf-typings/internal/testutil"
)

func fixture(t *testing.T, config any, response string) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	testutil.MockSF(t, root)
	binary := filepath.Join(root, "sf-typings")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate CLI test source")
	}
	build.Dir = filepath.Join(filepath.Dir(here), "..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	return root, binary, []string{"-config", configPath, "-out", filepath.Join(root, "generated")}
}

func runCLI(t *testing.T, root, binary, response string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "SF_TYPINGS_MOCK=1", "SF_TYPINGS_RESPONSE="+response, "SF_TYPINGS_LOG="+filepath.Join(root, "invoked"), "HOME="+root, "USERPROFILE="+root)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), e.ExitCode()
	}
	t.Fatal(err)
	return "", "", -1
}

func writeLocalOrgWithoutToken(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".sf")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alias.json"), []byte(`{"orgs":{"dev":"fixture@example.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture@example.test.json"), []byte(`{"username":"fixture@example.test","instanceUrl":"https://sample.my.salesforce.com"}`), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertUnchanged(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatalf("prior output=%q err=%v", got, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMain(m *testing.M) { testutil.MockMain(m) }
