package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func MockMain(m *testing.M) {
	if os.Getenv("SF_TYPINGS_MOCK") == "1" {
		argv := strings.Join(os.Args[1:], " ")
		if argv != "org display --target-org dev --json" && argv != "org auth show-access-token -o dev --no-prompt --json" {
			fmt.Fprintln(os.Stderr, "unexpected sf arguments")
			os.Exit(3)
		}
		log, err := os.OpenFile(os.Getenv("SF_TYPINGS_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(4)
		}
		if _, err := fmt.Fprintln(log, argv); err != nil {
			os.Exit(4)
		}
		if err := log.Close(); err != nil {
			os.Exit(4)
		}
		if argv == "org display --target-org dev --json" {
			if os.Getenv("SF_TYPINGS_RESPONSE") == "__display_untrusted__" {
				fmt.Print(`{"status":0,"result":{"instanceUrl":"https://evil.example","accessToken":"display-secret"}}`)
				os.Exit(0)
			}
			fmt.Print(`{"status":0,"result":{"instanceUrl":"https://sample.my.salesforce.com","accessToken":"display-secret"}}`)
			os.Exit(0)
		}
		if os.Getenv("SF_TYPINGS_RESPONSE") == "__fail__" {
			fmt.Fprintln(os.Stderr, "mock secret")
			os.Exit(7)
		}
		if os.Getenv("SF_TYPINGS_RESPONSE") == "__capped_stdout__" {
			fmt.Print(strings.Repeat("x", (1<<20)+1))
			os.Exit(0)
		}
		fmt.Print(os.Getenv("SF_TYPINGS_RESPONSE"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// MockSF installs this test executable as sf; MockMain handles its subprocess mode.
func MockSF(t *testing.T, root string) {
	t.Helper()
	mock := filepath.Join(root, "sf")
	if runtime.GOOS == "windows" {
		mock += ".exe"
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mock, content, 0700); err != nil {
		t.Fatal(err)
	}
}
