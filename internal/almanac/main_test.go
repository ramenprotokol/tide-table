package almanac

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points the time package at the tz database that ships with the Go
// toolchain (the same data time/tzdata embeds in the WebAssembly build), so
// results don't depend on the host system's zoneinfo.
func TestMain(m *testing.M) {
	if os.Getenv("ZONEINFO") == "" {
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "go env GOROOT failed:", err)
			os.Exit(2)
		}
		zip := filepath.Join(strings.TrimSpace(string(out)), "lib", "time", "zoneinfo.zip")
		if _, err := os.Stat(zip); err != nil {
			fmt.Fprintln(os.Stderr, "no zoneinfo.zip in GOROOT:", err)
			os.Exit(2)
		}
		os.Setenv("ZONEINFO", zip)
	}
	os.Exit(m.Run())
}
