package almanac

//go:generate go run ../../cmd/genzones

import (
	"sort"
	"strings"
)

// knownZone reports whether name is in Go's tz database, exactly as spelled.
// Checking the list first keeps behaviour the same everywhere: on a desktop
// with a case-insensitive file system, time.LoadLocation would otherwise find
// "europe/london" in the system's zoneinfo, while the WebAssembly build
// (embedded data only) would not.
func knownZone(name string) bool {
	i := sort.SearchStrings(zoneNames, name)
	return i < len(zoneNames) && zoneNames[i] == name
}

// suggestZone finds a zone that matches name ignoring case.
func suggestZone(name string) string {
	for _, z := range zoneNames {
		if strings.EqualFold(z, name) {
			return z
		}
	}
	return ""
}

// Zones lists the names worth suggesting in a picker: the Area/City zones
// plus UTC (legacy aliases such as US/Eastern are accepted but not offered).
func Zones() []string {
	out := []string{"UTC"}
	for _, z := range zoneNames {
		area, _, ok := strings.Cut(z, "/")
		if !ok {
			continue
		}
		switch area {
		case "Africa", "America", "Antarctica", "Arctic", "Asia", "Atlantic", "Australia", "Europe", "Indian", "Pacific":
			out = append(out, z)
		}
	}
	return out
}
