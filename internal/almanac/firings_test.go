package almanac

import (
	"strings"
	"testing"
	"time"
)

// A fixed "now" so the tests don't depend on the day they run.
var testNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC).UnixMilli()

func mustCompute(t *testing.T, req Request) (*Result, map[string]*runState) {
	t.Helper()
	if req.Now == 0 {
		req.Now = testNow
	}
	res, runs := compute(req)
	if res.Error != nil {
		t.Fatalf("Compute(%+v): %s", req, res.Error.Message)
	}
	return res, runs
}

func utc(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

func has(firings []int64, u int64) bool {
	for _, f := range firings {
		if f == u {
			return true
		}
	}
	return false
}

// seamOn finds the seam of a kind on a view-local date like "Sun 1 Nov 2026".
func seamOn(t *testing.T, v View, date, kind string) Seam {
	t.Helper()
	for _, s := range v.Seams {
		if s.Date == date && s.Kind == kind {
			return s
		}
	}
	t.Fatalf("%s: no %q seam on %s; seams: %+v", v.Zone, kind, date, v.Seams)
	return Seam{}
}

func noSeam(t *testing.T, v View, kind string) {
	t.Helper()
	for _, s := range v.Seams {
		if s.Kind == kind {
			t.Errorf("%s: unexpected %q seam on %s: %s", v.Zone, kind, s.Date, s.Text)
		}
	}
}

// Northern hemisphere: America/New_York changes at 02:00 local.
func TestNewYorkGapAndOverlap(t *testing.T) {
	// 02:30 falls in the spring-forward gap (02:00–02:59 on 14 Mar 2027).
	res, runs := mustCompute(t, Request{Expr: "30 2 * * *", Zones: []string{"America/New_York"}, From: "2026-09"})
	v := res.Views[0]
	if v.Days != 365 || v.Total != 364 {
		t.Errorf("30 2 * * *: %d days, %d firings; want 365 days, 364 firings", v.Days, v.Total)
	}
	s := seamOn(t, v, "Sun 14 Mar 2027", "skip")
	if !s.Red || !strings.Contains(s.Text, "02:00–02:59 doesn’t happen, so robfig/cron skips the 02:30 firing") {
		t.Errorf("skip seam text: %s", s.Text)
	}
	if has(runs["America/New_York"].firings, utc("2027-03-14T06:30:00Z")) || has(runs["America/New_York"].firings, utc("2027-03-14T07:30:00Z")) {
		t.Error("a firing exists on the gap day")
	}
	noSeam(t, v, "double") // 02:30 happens once in November: the repeated hour is 01:00–01:59
	seamOn(t, v, "Sun 1 Nov 2026", "quiet")

	// 01:30 falls in the fall-back overlap (01:00–01:59 on 1 Nov 2026): it runs twice.
	res, runs = mustCompute(t, Request{Expr: "30 1 * * *", Zones: []string{"America/New_York"}, From: "2026-09"})
	v = res.Views[0]
	if v.Total != 366 {
		t.Errorf("30 1 * * *: %d firings, want 366", v.Total)
	}
	f := runs["America/New_York"].firings
	if !has(f, utc("2026-11-01T05:30:00Z")) || !has(f, utc("2026-11-01T06:30:00Z")) {
		t.Error("want firings at 01:30 EDT (05:30Z) and 01:30 EST (06:30Z)")
	}
	if s := seamOn(t, v, "Sun 1 Nov 2026", "double"); !strings.Contains(s.Text, "fires at 01:30 both times, an hour apart") {
		t.Errorf("double seam text: %s", s.Text)
	}
	if v.Counts[61] != 2 { // 1 Nov is day 61 of an almanac starting 1 Sep
		t.Errorf("1 Nov count = %d, want 2", v.Counts[61])
	}

	// A dense job: the seam names the run of minutes, not sixty times.
	res, _ = mustCompute(t, Request{Expr: "* 1-3 * * *", Zones: []string{"America/New_York"}, From: "2026-09"})
	if s := seamOn(t, res.Views[0], "Sun 1 Nov 2026", "double"); !strings.Contains(s.Text, "fires both times at each minute from 01:00 to 01:59") {
		t.Errorf("dense double text: %s", s.Text)
	}
	if s := seamOn(t, res.Views[0], "Sun 14 Mar 2027", "skip"); !strings.Contains(s.Text, "skips 60 firings (each minute from 02:00 to 02:59)") {
		t.Errorf("dense skip text: %s", s.Text)
	}

	// An hourly job: 25 firings on the long day, 23 on the short one.
	res, _ = mustCompute(t, Request{Expr: "0 * * * *", Zones: []string{"America/New_York"}, From: "2026-09"})
	v = res.Views[0]
	if v.Counts[61] != 25 || v.Counts[194] != 23 || v.Counts[60] != 24 {
		t.Errorf("hourly counts: 1 Nov %d, 14 Mar %d, 31 Oct %d; want 25, 23, 24", v.Counts[61], v.Counts[194], v.Counts[60])
	}
}

// Europe/London changes at 01:00 GMT, so 01:30 is hit both ways.
func TestLondonGapAndOverlap(t *testing.T) {
	res, runs := mustCompute(t, Request{Expr: "30 1 * * *", Zones: []string{"Europe/London"}, From: "2026-09"})
	v := res.Views[0]
	seamOn(t, v, "Sun 25 Oct 2026", "double")
	seamOn(t, v, "Sun 28 Mar 2027", "skip")
	if v.Total != 365 {
		t.Errorf("total %d, want 365 (one skipped, one repeated)", v.Total)
	}
	f := runs["Europe/London"].firings
	if !has(f, utc("2026-10-25T00:30:00Z")) || !has(f, utc("2026-10-25T01:30:00Z")) {
		t.Error("want 01:30 BST and 01:30 GMT on 25 Oct")
	}
	if has(f, utc("2027-03-28T01:30:00Z")) || has(f, utc("2027-03-28T00:30:00Z")) {
		t.Error("01:30 on 28 Mar 2027 doesn't exist and must not fire")
	}
	// Marks on the dial: 01:30 (minute 90) as both a skip and a double.
	var skip, dbl bool
	var marks []Mark
	for _, s := range v.Seams {
		marks = append(marks, s.Marks...)
	}
	for _, m := range marks {
		skip = skip || m.Minute == 90 && m.Kind == "skip"
		dbl = dbl || m.Minute == 90 && m.Kind == "double"
	}
	if !skip || !dbl {
		t.Errorf("marks %+v", marks)
	}
}

// Southern hemisphere: Australia/Sydney springs forward in October and falls
// back in April.
func TestSydneyGapAndOverlap(t *testing.T) {
	res, runs := mustCompute(t, Request{Expr: "30 2 * * *", Zones: []string{"Australia/Sydney"}, From: "2026-09"})
	v := res.Views[0]
	s := seamOn(t, v, "Sun 4 Oct 2026", "skip")
	if !strings.Contains(s.Text, "AEST becomes AEDT; daylight saving begins") {
		t.Errorf("skip text: %s", s.Text)
	}
	seamOn(t, v, "Sun 4 Apr 2027", "double")
	f := runs["Australia/Sydney"].firings
	if !has(f, utc("2027-04-03T15:30:00Z")) || !has(f, utc("2027-04-03T16:30:00Z")) {
		t.Error("want 02:30 AEDT (15:30Z) and 02:30 AEST (16:30Z) on 4 Apr 2027")
	}
	if v.Total != 365 {
		t.Errorf("total %d, want 365", v.Total)
	}
}

// Australia/Lord_Howe moves its clocks by 30 minutes, not an hour.
func TestLordHoweHalfHour(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "15 2 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	s := seamOn(t, res.Views[0], "Sun 4 Oct 2026", "skip")
	if !strings.Contains(s.Text, "forward 30 minutes") || !strings.Contains(s.Text, "02:00–02:29") {
		t.Errorf("Lord Howe skip text: %s", s.Text)
	}
	res, runs := mustCompute(t, Request{Expr: "45 1 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	s = seamOn(t, res.Views[0], "Sun 4 Apr 2027", "double")
	if !strings.Contains(s.Text, "30 minutes apart") {
		t.Errorf("Lord Howe double text: %s", s.Text)
	}
	f := runs["Australia/Lord_Howe"].firings
	if !has(f, utc("2027-04-03T14:45:00Z")) || !has(f, utc("2027-04-03T15:15:00Z")) {
		t.Error("want 01:45 +11 and 01:45 +10:30 on 4 Apr 2027")
	}
	// 01:15 is outside the 01:30–01:59 overlap: once only.
	res, _ = mustCompute(t, Request{Expr: "15 1 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	noSeam(t, res.Views[0], "double")
}

// robfig/cron's hour-by-hour search lands on :30 after a 30-minute change, so
// on both change days it passes over 12:00, which does happen. The seam must
// say so, not claim the time is missing or repeated.
func TestLordHoweQuirkExplained(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 12 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	v := res.Views[0]
	if v.Total != 363 {
		t.Errorf("total %d, want 363 (12:00 passed over on both change days)", v.Total)
	}
	for _, c := range []struct{ date, steps string }{
		{"Sun 4 Oct 2026", "(01:00, 02:30, 03:30, …)"},
		{"Sun 4 Apr 2027", "(01:00, 01:30, 02:30, …)"},
	} {
		s := seamOn(t, v, c.date, "skip")
		want := "12:00 does happen that day, but robfig/cron skips it: its search moves an hour at a time " + c.steps + ", so after this change of 30 minutes it never looks at 12:00. This is a library quirk, not a missing time."
		if !strings.Contains(s.Text, want) {
			t.Errorf("%s:\n got %s\nwant …%s", c.date, s.Text, want)
		}
		for _, bad := range []string{"doesn’t happen", "happens twice"} {
			if strings.Contains(s.Text, bad) {
				t.Errorf("%s: says %q: %s", c.date, bad, s.Text)
			}
		}
		if s.Label != "12:00 skipped" {
			t.Errorf("%s label %q", c.date, s.Label)
		}
	}

	// 01:30 fires as usual on 4 Oct, and then an extra run at 02:30.
	res, _ = mustCompute(t, Request{Expr: "30 1 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	v = res.Views[0]
	s := seamOn(t, v, "Sun 4 Oct 2026", "extra")
	if s.Label != "02:30 extra run" || !strings.Contains(s.Text, "robfig/cron also fires at 02:30, which the expression doesn’t name: an extra run.") {
		t.Errorf("extra run seam: %q %s", s.Label, s.Text)
	}
	if strings.Contains(string(res.JSON()), "moved") {
		t.Error("an extra run is described as moved")
	}
	if v.Counts[33] != 2 {
		t.Errorf("4 Oct count %d, want 2", v.Counts[33])
	}

	// A time that really is in the gap still reads as missing.
	res, _ = mustCompute(t, Request{Expr: "15 2 * * *", Zones: []string{"Australia/Lord_Howe"}, From: "2026-09"})
	if s := seamOn(t, res.Views[0], "Sun 4 Oct 2026", "skip"); !strings.Contains(s.Text, "02:00–02:29 doesn’t happen, so robfig/cron skips the 02:15 firing.") {
		t.Errorf("gap text: %s", s.Text)
	}

	// The drift can carry to a later day: @monthly passes over 1 November.
	res, _ = mustCompute(t, Request{Expr: "@monthly", Zones: []string{"Australia/Lord_Howe"}, From: "2028-06"})
	v = res.Views[0]
	if v.Total != 10 {
		t.Errorf("@monthly from 2028-06: %d firings, want 10", v.Total)
	}
	s = seamOn(t, v, "Wed 1 Nov 2028", "skip")
	if !s.Red || !strings.Contains(s.Text, "robfig/cron looked for this run by searching on from the previous run, Sun 1 Oct 2028 at 00:00, across the clock change on Sun 1 Oct 2028 (+1030 becomes +11)") {
		t.Errorf("carried skip: %s", s.Text)
	}
	for _, sm := range v.Seams {
		if strings.Contains(sm.Text, "No clock change explains it") {
			t.Errorf("unexplained seam on %s: %s", sm.Date, sm.Text)
		}
	}
}

// Asunción's midnight change sends the library's month search past all of
// November 2023: one seam for the run of days, not thirty.
func TestCarriedSkipsGrouped(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 12 * 11 *", Zones: []string{"America/Asuncion"}, From: "2023-01"})
	v := res.Views[0]
	if v.Total != 0 {
		t.Errorf("total %d, want 0", v.Total)
	}
	s := seamOn(t, v, "Wed 1 Nov 2023", "skip")
	if s.Label != "12:00 skipped, 30 days" || !strings.Contains(s.Text, "From Wed 1 Nov 2023 to Thu 30 Nov 2023 (30 days), robfig/cron skips 12:00 each day.") || !strings.Contains(s.Text, "from the start of this almanac") {
		t.Errorf("grouped seam: %q %s", s.Label, s.Text)
	}
	red := 0
	for _, sm := range v.Seams {
		if sm.Red {
			red++
		}
	}
	if red != 1 {
		t.Errorf("%d red seams, want 1", red)
	}
}

// Pacific/Chatham (UTC+12:45) jumps from 02:45 to 03:45.
func TestChathamQuarterHourZone(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 3 * * *", Zones: []string{"Pacific/Chatham"}, From: "2026-09"})
	v := res.Views[0]
	seamOn(t, v, "Sun 27 Sep 2026", "skip")
	seamOn(t, v, "Sun 4 Apr 2027", "double")
}

// America/Havana changes clocks at midnight, so 00:00 itself is missing one
// day and repeated another.
func TestMidnightChange(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 0 * * *", Zones: []string{"America/Havana"}, From: "2026-09"})
	v := res.Views[0]
	seamOn(t, v, "Sun 14 Mar 2027", "skip")
	seamOn(t, v, "Sun 1 Nov 2026", "double")
	if v.Counts[194] != 0 {
		t.Errorf("14 Mar count %d, want 0", v.Counts[194])
	}
	// The almanac day still starts at the first instant of that date (01:00).
	loc, _ := LoadZone("America/Havana")
	if got, want := startOfDay(loc, 2027, 3, 14), utc("2027-03-14T05:00:00Z"); got != want {
		t.Errorf("startOfDay(Havana, 14 Mar 2027) = %s, want %s", time.Unix(got, 0).UTC(), time.Unix(want, 0).UTC())
	}
}

// Shared mode: the job runs on New York's clock; London reads it at 14:00,
// except in the weeks when only one of the two zones is on summer time.
func TestSharedModeShifts(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 9 * * 1-5", Zones: []string{"America/New_York", "Europe/London"}, Mode: "shared", From: "2026-09"})
	ny, ldn := res.Views[0], res.Views[1]
	if ldn.RunZone != "America/New_York" {
		t.Fatalf("London runs on %s", ldn.RunZone)
	}
	if ny.Total != ldn.Total {
		t.Errorf("same instants, different totals: %d vs %d", ny.Total, ldn.Total)
	}
	dial := map[int]int{}
	for _, d := range ldn.Dial {
		dial[d[0]] = d[1]
	}
	// 26–30 Oct 2026 (5 weekdays) and 15–26 Mar 2027 (10 weekdays) read 13:00.
	if dial[13*60] != 15 || dial[14*60] != ldn.Total-15 || len(dial) != 2 {
		t.Errorf("London dial %v, total %d", dial, ldn.Total)
	}
	for _, c := range []struct{ date, label string }{
		{"Sun 25 Oct 2026", "reads 1 h earlier"},
		{"Sun 1 Nov 2026", "reads 1 h later"},
		{"Sun 14 Mar 2027", "reads 1 h earlier"},
		{"Sun 28 Mar 2027", "reads 1 h later"},
	} {
		if s := seamOn(t, ldn, c.date, "shift"); s.Label != c.label || !s.Red {
			t.Errorf("%s: label %q red %v, want %q", c.date, s.Label, s.Red, c.label)
		}
	}
	// On New York's own page the job is untouched by its clock changes.
	noSeam(t, ny, "skip")
	noSeam(t, ny, "double")
	seamOn(t, ny, "Sun 14 Mar 2027", "quiet")
}

// Each mode: every zone runs its own copy of the job.
func TestEachMode(t *testing.T) {
	zones := []string{"America/New_York", "Europe/London", "Australia/Sydney"}
	res, runs := mustCompute(t, Request{Expr: "30 1 * * *", Zones: zones, Mode: "each", From: "2026-09"})
	if len(runs) != 3 {
		t.Fatalf("%d run zones, want 3", len(runs))
	}
	for i, v := range res.Views {
		if v.RunZone != zones[i] {
			t.Errorf("view %s runs on %s", v.Zone, v.RunZone)
		}
	}
	seamOn(t, res.Views[0], "Sun 1 Nov 2026", "double")
	seamOn(t, res.Views[1], "Sun 25 Oct 2026", "double")
	seamOn(t, res.Views[1], "Sun 28 Mar 2027", "skip")
	noSeam(t, res.Views[2], "skip") // Sydney's gap is 02:00–02:59
}

// The next-firings list flags both runs of a repeated wall time.
func TestNextFlagsRepeats(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC).UnixMilli()
	res, _ := mustCompute(t, Request{Expr: "30 1 * * *", Zones: []string{"America/New_York", "Europe/London"}, Now: now})
	ny, ldn := res.Views[0].Next, res.Views[1].Next
	if len(ny) != NextCount {
		t.Fatalf("%d next firings, want %d", len(ny), NextCount)
	}
	if ny[0].Date != "Sun 1 Nov 2026" || ny[0].Time != "01:30" || ny[0].Abbr != "EDT" || !strings.HasPrefix(ny[0].Note, "first of two 01:30") {
		t.Errorf("first: %+v", ny[0])
	}
	if ny[1].Date != "Sun 1 Nov 2026" || ny[1].Time != "01:30" || ny[1].Abbr != "EST" || !strings.HasPrefix(ny[1].Note, "01:30 again") {
		t.Errorf("second: %+v", ny[1])
	}
	if ny[2].Note != "" {
		t.Errorf("third should be plain: %+v", ny[2])
	}
	if ldn[0].Time != "05:30" || ldn[1].Time != "06:30" || ldn[0].Abbr != "GMT" {
		t.Errorf("London readings: %+v %+v", ldn[0], ldn[1])
	}
}

func equal64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
