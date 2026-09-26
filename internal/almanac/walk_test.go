package almanac

import (
	"fmt"
	"hash/fnv"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// The walk's promise: every firing is exactly what robfig/cron's Next returns
// when it is called the plainest way, with the real zone and from each firing
// it returned (libraryNext). The almanac calls it through fastNext, which
// hands Next a fixed-offset zone where it can prove the answer is the same.
// These tests check that promise across the whole tz database.

func mustSpec(t testing.TB, expr string, loc *time.Location) cron.SpecSchedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	spec := *s.Spec
	spec.Location = loc
	return spec
}

func mustZone(t testing.TB, name string) *time.Location {
	t.Helper()
	loc, err := LoadZone(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// window is a year from the 1st of a month, on the zone's own calendar.
func window(loc *time.Location, y int, m time.Month) (int64, int64) {
	return startOfDay(loc, y, m, 1), startOfDay(loc, y+1, m, 1)
}

func sameWalk(x, y walkOut) bool {
	if !equal64(x.firings, y.firings) || x.truncated != y.truncated || len(x.loops) != len(y.loops) {
		return false
	}
	for i := range x.loops {
		if x.loops[i] != y.loops[i] {
			return false
		}
	}
	return true
}

func describeDiff(lib, fast walkOut, loc *time.Location) string {
	ls, fs := map[int64]bool{}, map[int64]bool{}
	for _, u := range lib.firings {
		ls[u] = true
	}
	for _, u := range fast.firings {
		fs[u] = true
	}
	var onlyL, onlyF []string
	for _, u := range lib.firings {
		if !fs[u] && len(onlyL) < 4 {
			onlyL = append(onlyL, time.Unix(u, 0).In(loc).Format("2006-01-02 15:04 MST"))
		}
	}
	for _, u := range fast.firings {
		if !ls[u] && len(onlyF) < 4 {
			onlyF = append(onlyF, time.Unix(u, 0).In(loc).Format("2006-01-02 15:04 MST"))
		}
	}
	return fmt.Sprintf("library %d firings (%d loops, truncated %v), fast %d (%d loops, truncated %v); only library %v; only fast %v",
		len(lib.firings), len(lib.loops), lib.truncated, len(fast.firings), len(fast.loops), fast.truncated, onlyL, onlyF)
}

// compareWalks walks [a, b) both ways and reports any difference.
func compareWalks(spec cron.SpecSchedule, loc *time.Location, a, b int64) (lib walkOut, fast walkOut, fn *fastNext, ok bool) {
	lib = walkWith(libraryNext(spec, loc), a, b)
	fn = newFastNext(spec, loc)
	fast = walkWith(fn.next, a, b)
	return lib, fast, fn, sameWalk(lib, fast)
}

// The expressions the review found sparse-restriction bugs with, plus the
// ones that show gaps, overlaps and robfig/cron's re-fire loop.
var sweepExprs = []string{
	"@monthly", "0 12 * 11 *", "0 0 1 * *", "5 * * * *", "59 23 * * *", "0 0 * * *",
	"30 0 * 3,4,9,10 *", "0 12 * * 0", "30 2 * * *", "50 3 * * *", "0 9 1-7 * 1", "0 12 * * *",
}

// sampleWindows picks n windows for a zone deterministically: one from the
// years Go's tz table covers (1970–2037), one from the rule-based years
// (2038–2199), then any year; each from a month picked the same way.
func sampleWindows(zone string, n int) [][2]int {
	h := fnv.New64a()
	h.Write([]byte(zone))
	x := h.Sum64()
	next := func(k uint64) int {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return int(x % k)
	}
	var out [][2]int
	for i := 0; i < n; i++ {
		var y int
		switch i % 3 {
		case 0:
			y = MinYear + next(2037-MinYear+1)
		case 1:
			y = 2038 + next(uint64(MaxYear-2038+1))
		default:
			y = MinYear + next(uint64(MaxYear-MinYear+1))
		}
		out = append(out, [2]int{y, 1 + next(12)})
	}
	return out
}

type sweepStats struct {
	windows, firings, loops, fast, real atomic.Int64
}

// sweep compares the two walks for every zone and window it is given, in
// parallel, and returns the mismatches (at most 30 described). With progress
// set it also logs each mismatch and each finished zone as it goes (visible
// with -v), so a long run that is stopped early still leaves a record.
func sweep(t *testing.T, zones []string, windows func(zone string) [][2]int, exprs func(zone string, i int) []string, progress bool) (*sweepStats, []string) {
	var st sweepStats
	var mu sync.Mutex
	var bad []string
	var nbad, ndone atomic.Int64
	jobs := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for z := range jobs {
				loc := mustZone(t, z)
				for i, win := range windows(z) {
					a, b := window(loc, win[0], time.Month(win[1]))
					for _, e := range exprs(z, i) {
						lib, fast, fn, ok := compareWalks(mustSpec(t, e, loc), loc, a, b)
						st.windows.Add(1)
						st.firings.Add(int64(len(lib.firings)))
						st.loops.Add(int64(len(lib.loops)))
						st.fast.Add(int64(fn.fastCalls))
						st.real.Add(int64(fn.realCalls))
						if !ok {
							msg := fmt.Sprintf("%-18s %-32s from %d-%02d: %s", e, z, win[0], win[1], describeDiff(lib, fast, loc))
							if progress {
								t.Logf("MISMATCH %s", msg)
							}
							if nbad.Add(1) <= 30 {
								mu.Lock()
								bad = append(bad, msg)
								mu.Unlock()
							}
						}
					}
				}
				if progress {
					t.Logf("zone %d/%d done: %s (%d walks, %d mismatches so far)", ndone.Add(1), len(zones), z, st.windows.Load(), nbad.Load())
				}
			}
		}()
	}
	for _, z := range zones {
		jobs <- z
	}
	close(jobs)
	wg.Wait()
	if n := nbad.Load(); n > int64(len(bad)) {
		bad = append(bad, fmt.Sprintf("… %d mismatches in all", n))
	}
	return &st, bad
}

// Every zone in the embedded tz database (all 598 names, links included),
// three sampled windows between 1970 and 2199 each, and the sparse
// expressions above. The hourly "5 * * * *" is the costly one, so it runs on
// one of each zone's three windows (rotating). The full sweep is
// TestWalkSweepExhaustive.
func TestWalkMatchesLibraryInEveryZone(t *testing.T) {
	if testing.Short() {
		t.Skip("slow; run without -short")
	}
	start := time.Now()
	st, bad := sweep(t, zoneNames,
		func(z string) [][2]int { return sampleWindows(z, 3) },
		func(z string, i int) []string {
			var out []string
			for _, e := range sweepExprs {
				if e == "5 * * * *" && i != int(fnv32(z)%3) {
					continue
				}
				out = append(out, e)
			}
			return out
		}, false)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("%d zones, %d zone-windows × expressions, %d firings compared (%d re-fire loops), Next calls: %d fixed-offset, %d real zone; %s",
		len(zoneNames), st.windows.Load(), st.firings.Load(), st.loops.Load(), st.fast.Load(), st.real.Load(), time.Since(start).Round(time.Millisecond))
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// The exhaustive sweep: every zone, a window from every January 1970–2199
// (and, with TIDE_SWEEP_MONTHS=all, from every month), every expression
// above. It takes hours of CPU (about 35 s of one core per zone with
// daylight saving), so it is off by default and not part of npm test; the
// sampled test above is the gate. Run it with
//
//	TIDE_SWEEP=1 go test ./internal/almanac -run TestWalkSweepExhaustive -timeout 0 -v
//
// TIDE_SWEEP_ZONES (comma-separated), TIDE_SWEEP_YEARS (from-to) and
// TIDE_SWEEP_EXPRS (semicolon-separated) narrow it. With -v it logs each
// mismatch and each finished zone as it goes.
func TestWalkSweepExhaustive(t *testing.T) {
	if os.Getenv("TIDE_SWEEP") == "" {
		t.Skip("set TIDE_SWEEP=1 to run the exhaustive sweep")
	}
	zones := zoneNames
	if z := os.Getenv("TIDE_SWEEP_ZONES"); z != "" {
		zones = strings.Split(z, ",")
	}
	y0, y1 := MinYear, MaxYear
	if y := os.Getenv("TIDE_SWEEP_YEARS"); y != "" {
		a, b, _ := strings.Cut(y, "-")
		y0, _ = strconv.Atoi(a)
		y1, _ = strconv.Atoi(b)
	}
	exprs := sweepExprs
	if e := os.Getenv("TIDE_SWEEP_EXPRS"); e != "" {
		exprs = strings.Split(e, ";")
	}
	months := []int{1}
	if os.Getenv("TIDE_SWEEP_MONTHS") == "all" {
		months = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	}
	var wins [][2]int
	for y := y0; y <= y1; y++ {
		for _, m := range months {
			wins = append(wins, [2]int{y, m})
		}
	}
	start := time.Now()
	st, bad := sweep(t, zones, func(string) [][2]int { return wins }, func(string, int) []string { return exprs }, true)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("%d zones × %d windows × %d expressions: %d walks, %d firings compared (%d re-fire loops), Next calls: %d fixed-offset, %d real zone; %d mismatches listed; %s",
		len(zones), len(wins), len(exprs), st.windows.Load(), st.firings.Load(), st.loops.Load(), st.fast.Load(), st.real.Load(), len(bad), time.Since(start).Round(time.Second))
}

// The cases the review found, pinned: each compares the two walks and checks
// the library's own count.
func TestWalkReviewRegressions(t *testing.T) {
	cases := []struct {
		expr, zone string
		y          int
		m          time.Month
		want       int // firings the library returns; -1 to skip the count
		loops      int
	}{
		// Half-hour changes on the 1st: the library carries a :30 drift into
		// the next month and passes over its 1st.
		{"@monthly", "Australia/Lord_Howe", 2028, 6, 10, 0},
		{"0 0 1 * *", "Australia/Lord_Howe", 2028, 1, 11, 0},
		// Asunción, where clocks change at midnight: nothing in November 2023.
		{"0 12 * 11 *", "America/Asuncion", 2023, 1, 0, 0},
		{"0 12 * 11 *", "America/Asuncion", 2017, 1, -1, 0},
		{"59 * * 9,11 *", "America/Asuncion", 2017, 1, -1, 0},
		{"0 0 * 11 *", "America/Asuncion", 2023, 1, -1, 0},
		// Monrovia's −0:44:30 offset: Truncate(time.Minute) on absolute time
		// broke the old stride; the library itself fires 8,759 times.
		{"5 * * * *", "Africa/Monrovia", 1970, 1, 8759, 0},
		{"0 0 * * *", "Africa/Monrovia", 1971, 1, 365, 0},
		{"30 9 * * *", "Africa/Monrovia", 1971, 6, -1, 0},
		// Havana, Hebron, Casey, Kathmandu, Lima: midnight changes and one-off
		// offset changes.
		{"0 0 * * *", "America/Havana", 2012, 1, -1, 0},
		{"0 0 * * *", "Asia/Hebron", 2011, 1, 363, 0},
		{"0 0 * * *", "Antarctica/Casey", 2020, 1, 366, 0},
		{"0 12 * * *", "Asia/Kathmandu", 1986, 1, 364, 0},
		{"0 0 * * *", "America/Lima", 1986, 1, -1, 0},
		// Where the old walk hit its backstop although the library doesn't loop.
		{"0 0 * * *", "America/Goose_Bay", 1987, 1, 364, 0},
		{"0 0 * * *", "America/Moncton", 1993, 1, 364, 0},
		{"0 0 1 * *", "America/Goose_Bay", 2009, 1, 11, 0},
		{"59 23 * * *", "America/Argentina/Cordoba", 1991, 1, 366, 0},
		{"59 23 * * *", "America/Argentina/Jujuy", 1990, 1, 366, 0},
		// Where the library itself loops: shown once, then resumed.
		{"50 3 * * *", "Pacific/Chatham", 2027, 1, -1, 1},
		{"50 3 * * *", "Pacific/Chatham", 2150, 1, -1, 1},
		{"30 0 * 3,4,9,10 *", "America/Goose_Bay", 1987, 1, -1, -1},
		// The original fixtures.
		{"0 12 * * *", "Australia/Lord_Howe", 2026, 9, 363, 0},
		{"0 12 * * 0", "Australia/Lord_Howe", 2026, 9, -1, 0},
		{"0 12 * * 0", "Asia/Beirut", 2026, 9, -1, 0},
	}
	for _, c := range cases {
		loc := mustZone(t, c.zone)
		a, b := window(loc, c.y, c.m)
		lib, fast, _, ok := compareWalks(mustSpec(t, c.expr, loc), loc, a, b)
		name := fmt.Sprintf("%s in %s from %d-%02d", c.expr, c.zone, c.y, c.m)
		if !ok {
			t.Errorf("%s: %s", name, describeDiff(lib, fast, loc))
		}
		if c.want >= 0 && len(lib.firings) != c.want {
			t.Errorf("%s: library returns %d firings, want %d", name, len(lib.firings), c.want)
		}
		if c.loops >= 0 && len(lib.loops) != c.loops {
			t.Errorf("%s: %d re-fire loops, want %d", name, len(lib.loops), c.loops)
		}
		if lib.truncated {
			t.Errorf("%s: walk hit its backstop", name)
		}
	}
}

// The densest schedules around every change in a year, in zones with
// half-hour, quarter-hour, midnight and double changes.
func TestWalkDenseAroundChanges(t *testing.T) {
	zones := []string{"America/New_York", "Australia/Lord_Howe", "Pacific/Chatham", "America/Havana", "America/Asuncion", "Africa/Casablanca", "Asia/Tehran"}
	for _, expr := range []string{"* * * * *", "*/7 1-3 * * *", "*/15 * * * *", "45-59 3 * * *"} {
		for _, z := range zones {
			loc := mustZone(t, z)
			for _, y := range []int{2026, 2100} {
				a, b := window(loc, y, 1)
				for _, c := range clockChanges(loc, a, b) {
					lo, hi := c.at-3*86400, c.at+3*86400
					lib, fast, _, ok := compareWalks(mustSpec(t, expr, loc), loc, lo, hi)
					if !ok {
						t.Errorf("%s in %s around %s: %s", expr, z, time.Unix(c.at, 0).UTC(), describeDiff(lib, fast, loc))
					}
					// Six days of absolute time hold 8,640 minutes whatever the
					// clocks do.
					if expr == "* * * * *" && len(lib.firings) != 6*1440 {
						t.Errorf("every minute in %s around %s: %d firings, want %d", z, time.Unix(c.at, 0).UTC(), len(lib.firings), 6*1440)
					}
				}
			}
		}
	}
}

// Next(02:50 on the day Chatham's clocks go back) returns 02:50 again: the
// library would re-fire in a tight loop. The walk records it once and moves on.
func TestChathamRefireLoop(t *testing.T) {
	loc := mustZone(t, "Pacific/Chatham")
	spec := mustSpec(t, "50 3 * * *", loc)
	f := time.Date(2027, 4, 3, 14, 5, 0, 0, time.UTC) // 02:50 +1245, Sun 4 Apr 2027
	if got := spec.Next(f); !got.Equal(f) {
		t.Fatalf("robfig/cron no longer loops here: Next(%s) = %s", f.In(loc), got.In(loc))
	}

	start := time.Now()
	res, runs := mustCompute(t, Request{Expr: "50 3 * * *", Zones: []string{"Pacific/Chatham"}, From: "2026-09"})
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("compute took %s", d)
	}
	v := res.Views[0]
	if res.Truncated || v.Total > 366 {
		t.Fatalf("truncated %v, total %d", res.Truncated, v.Total)
	}
	r := runs["Pacific/Chatham"]
	if len(r.loops) != 1 || r.loops[0].at != f.Unix() {
		t.Fatalf("loops %+v", r.loops)
	}
	s := seamOn(t, v, "Sun 4 Apr 2027", "loop")
	for _, want := range []string{
		"robfig/cron also fires at 02:50, which the expression doesn’t name: an extra run.",
		"After the 02:50 run, robfig/cron would re-fire in a tight loop until 03:00. This is a library quirk; shown once here.",
	} {
		if !strings.Contains(s.Text, want) {
			t.Errorf("seam text lacks %q:\n%s", want, s.Text)
		}
	}
	if s.Label != "02:50 re-fires" || !s.Red {
		t.Errorf("label %q red %v", s.Label, s.Red)
	}
	// 4 Apr: the extra 02:50 run and the real 03:50 one.
	if v.Counts[215] != 2 {
		t.Errorf("4 Apr count %d, want 2", v.Counts[215])
	}
	for _, sm := range v.Seams {
		if strings.Contains(sm.Text, "No clock change explains it") {
			t.Errorf("unexplained seam on %s: %s", sm.Date, sm.Text)
		}
	}

	// The next-ten list: distinct times, the loop noted once.
	now := time.Date(2027, 4, 3, 12, 0, 0, 0, time.UTC).UnixMilli()
	res, _ = mustCompute(t, Request{Expr: "50 3 * * *", Zones: []string{"Pacific/Chatham"}, From: "2027-04", Now: now})
	next := res.Views[0].Next
	seen := map[int64]bool{}
	for _, n := range next {
		if seen[n.At] {
			t.Errorf("repeated next firing %s %s", n.Date, n.Time)
		}
		seen[n.At] = true
	}
	if len(next) != NextCount || next[0].Time != "02:50" || !strings.Contains(next[0].Note, "tight loop until 03:00") || next[1].Time != "03:50" || next[1].Date != "Sun 4 Apr 2027" {
		t.Errorf("next: %+v", next[:2])
	}
}

// If the backstop ever stops a walk, the result says so.
func TestBackstopIsReported(t *testing.T) {
	old := maxWalk
	maxWalk = 100
	res, _ := mustCompute(t, Request{Expr: "0 * * * *", Zones: []string{"UTC"}, From: "2026-09"})
	maxWalk = old
	if !res.Truncated || res.Views[0].Total != 100 {
		t.Errorf("truncated %v, total %d", res.Truncated, res.Views[0].Total)
	}
	if !strings.Contains(string(res.JSON()), `"truncated":true`) {
		t.Error("JSON lacks truncated")
	}
	res, _ = mustCompute(t, Request{Expr: "0 0 * * *", Zones: []string{"UTC"}, From: "2026-09"})
	if res.Truncated {
		t.Error("a 365-firing walk reported as truncated")
	}
}
