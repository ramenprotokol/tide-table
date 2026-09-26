package almanac

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zones_gen.go must match the toolchain's tz database, which is what the
// WebAssembly build embeds. If this fails after a Go upgrade, run
// `go generate ./...`.
func TestZoneListMatchesToolchain(t *testing.T) {
	zipPath := os.Getenv("ZONEINFO")
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/") {
			names = append(names, f.Name)
		}
	}
	if len(names) != len(zoneNames) {
		t.Fatalf("toolchain has %d zones, zones_gen.go has %d: run go generate ./...", len(names), len(zoneNames))
	}
	for _, n := range names {
		if !knownZone(n) {
			t.Errorf("%s missing from zones_gen.go", n)
		}
		if _, err := time.LoadLocation(n); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(zipPath), "update.bash"))
	if err == nil && !strings.Contains(string(b), "DATA="+TZDataVersion+"\n") {
		t.Errorf("TZDataVersion %s doesn't match update.bash", TZDataVersion)
	}
}

func TestZoneSuggestions(t *testing.T) {
	z := Zones()
	if z[0] != "UTC" || len(z) < 300 {
		t.Fatalf("Zones() = %d names starting %q", len(z), z[0])
	}
	for _, n := range z {
		if !knownZone(n) {
			t.Errorf("suggested %s isn't known", n)
		}
		if strings.HasPrefix(n, "US/") || strings.HasPrefix(n, "Etc/") {
			t.Errorf("legacy name %s suggested", n)
		}
	}
	if _, err := LoadZone("US/Eastern"); err != nil {
		t.Errorf("legacy names are still accepted: %v", err)
	}
	if _, err := LoadZone("europe/london"); err == nil || !strings.Contains(err.Error(), "Did you mean Europe/London?") {
		t.Errorf("lower-case name: %v", err)
	}
}
