package almanac

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLeapYears(t *testing.T) {
	// 29 February: nothing in a 12-month window without one, and the next
	// firing is found in 2028.
	res, _ := mustCompute(t, Request{Expr: "0 12 29 2 *", Zones: []string{"UTC"}, From: "2026-09"})
	v := res.Views[0]
	if v.Total != 0 || res.Never {
		t.Errorf("2026-09 window: total %d never %v", v.Total, res.Never)
	}
	if len(v.Next) == 0 || v.Next[0].Date != "Tue 29 Feb 2028" || v.Next[0].Time != "12:00" {
		t.Errorf("next firing %+v, want Tue 29 Feb 2028 12:00", v.Next)
	}

	// A window starting January 2028 has 366 days and a 29-day February.
	res, _ = mustCompute(t, Request{Expr: "0 12 29 2 *", Zones: []string{"Europe/Paris"}, From: "2028-01"})
	v = res.Views[0]
	if v.Days != 366 || v.Months[1].Days != 29 || v.Months[1].Label != "Feb" {
		t.Errorf("2028: %d days, February %d days", v.Days, v.Months[1].Days)
	}
	if v.Total != 1 || v.Counts[31+28] != 1 {
		t.Errorf("2028: total %d, count on 29 Feb %d", v.Total, v.Counts[59])
	}

	// A window that crosses a leap day, starting mid-year.
	res, _ = mustCompute(t, Request{Expr: "0 0 * * *", Zones: []string{"UTC"}, From: "2027-03"})
	if v := res.Views[0]; v.Days != 366 || v.Total != 366 {
		t.Errorf("Mar 2027 – Feb 2028: %d days, %d firings; want 366, 366", v.Days, v.Total)
	}

	// 2100 is not a leap year (divisible by 100, not by 400); 2400 would be.
	res, _ = mustCompute(t, Request{Expr: "0 12 29 2 *", Zones: []string{"UTC"}, From: "2100-01", Now: time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli()})
	v = res.Views[0]
	if v.Days != 365 || v.Months[1].Days != 28 || v.Total != 0 {
		t.Errorf("2100: %d days, February %d, total %d", v.Days, v.Months[1].Days, v.Total)
	}
	if len(v.Next) == 0 || v.Next[0].Date != "Fri 29 Feb 2104" {
		t.Errorf("next after mid-2099: %+v, want Fri 29 Feb 2104", v.Next)
	}

	// 30 February never happens; the library gives up after five years.
	res, _ = mustCompute(t, Request{Expr: "0 0 30 2 *", Zones: []string{"UTC"}})
	if !res.Never || res.Views[0].NextNote == "" || !strings.Contains(res.Description, "never fires") {
		t.Errorf("30 Feb: never %v note %q description %q", res.Never, res.Views[0].NextNote, res.Description)
	}

	// The 31st: 7 months of 12.
	res, _ = mustCompute(t, Request{Expr: "0 0 31 * *", Zones: []string{"UTC"}, From: "2027-01"})
	if res.Views[0].Total != 7 {
		t.Errorf("31st of the month: %d firings, want 7", res.Views[0].Total)
	}
}

func TestInputErrors(t *testing.T) {
	cases := []struct {
		req   Request
		kind  string
		index int
		want  string
	}{
		{Request{Expr: "0 25 * * *", Zones: []string{"UTC"}}, "expr", -1, "Hour “25”"},
		{Request{Expr: "0 9 * * *"}, "zone", 0, "at least one time zone"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC", "Europe/London", "Asia/Tokyo", "Asia/Dubai"}}, "zone", 3, "At most 3"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC", "Mars/Olympus_Mons"}}, "zone", 1, "Unknown time zone “Mars/Olympus_Mons”"},
		{Request{Expr: "0 9 * * *", Zones: []string{"europe/london"}}, "zone", 0, "case-sensitive"},
		{Request{Expr: "0 9 * * *", Zones: []string{"Local"}}, "zone", 0, "depends on the machine"},
		{Request{Expr: "0 9 * * *", Zones: []string{"../../etc/passwd"}}, "zone", 0, "isn’t an IANA zone name"},
		{Request{Expr: "0 9 * * *", Zones: []string{"Europe/London", " Europe/London "}}, "zone", 1, "already zone 1"},
		{Request{Expr: "0 9 * * *", Zones: []string{""}}, "zone", 0, "Choose a time zone"},
		{Request{Expr: "0 9 * * *", Zones: []string{strings.Repeat("A", 65)}}, "zone", 0, "too long"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC"}, From: "2027-13"}, "from", -1, "01 to 12"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC"}, From: "27-01"}, "from", -1, "should look like"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC"}, From: "1969-12"}, "from", -1, "from 1970 to 2199"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC"}, From: "2200-01"}, "from", -1, "from 1970 to 2199"},
		{Request{Expr: "0 9 * * *", Zones: []string{"UTC"}, Mode: "some"}, "zone", -1, "Mode must be"},
	}
	for _, c := range cases {
		c.req.Now = testNow
		res := Compute(c.req)
		if res.Error == nil {
			t.Errorf("%+v: no error", c.req)
			continue
		}
		e := res.Error
		if e.Kind != c.kind || (c.kind == "zone" && e.Index != c.index) || !strings.Contains(e.Message, c.want) {
			t.Errorf("%+v: got %+v, want kind %s index %d containing %q", c.req, e, c.kind, c.index, c.want)
		}
	}
}

// The work is capped: three zones of an every-minute job is the most there is.
func TestWorkIsBounded(t *testing.T) {
	res, runs := mustCompute(t, Request{Expr: "* * * * *", Zones: []string{"America/New_York", "Europe/London", "Australia/Sydney"}, Mode: "each", From: "2027-09"})
	if len(runs) != 3 {
		t.Fatalf("%d runs", len(runs))
	}
	for _, v := range res.Views {
		// Sep 2027 – Aug 2028 has 366 local days of 1,440 minutes; the skipped
		// hour and the repeated hour cancel out.
		if v.Days != 366 || v.Total != 366*1440 {
			t.Errorf("%s: %d days, %d firings, want 366 and 527,040", v.Zone, v.Days, v.Total)
		}
		if len(v.Dial) != 1440 || len(v.Bins) != v.Days {
			t.Errorf("%s: dial %d entries, bins %d", v.Zone, len(v.Dial), len(v.Bins))
		}
		for _, r := range runs {
			if len(r.firings) >= maxWalk {
				t.Errorf("%s hit the walk backstop", r.name)
			}
		}
	}
	if res.Walked > 3*(366+2)*1440 {
		t.Errorf("walked %d firings", res.Walked)
	}
}

func TestBinsEncoding(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 0,6,23 * * *", Zones: []string{"UTC"}, From: "2027-01"})
	v := res.Views[0]
	// 00:00 → bin 0 (digit 0 bit 0); 06:00 → bin 24 (digit 6 bit 0); 23:00 → bin 92 (digit 23 bit 0).
	want := "100000100000000000000001"
	if v.Bins[0] != want || v.Counts[0] != 3 {
		t.Errorf("bins %q count %d, want %q 3", v.Bins[0], v.Counts[0], want)
	}
	res, _ = mustCompute(t, Request{Expr: "45 1 * * *", Zones: []string{"UTC"}, From: "2027-01"})
	if got := res.Views[0].Bins[0]; got != "080000000000000000000000" { // bin 7 → digit 1 bit 3
		t.Errorf("01:45 bins %q", got)
	}
}

func TestMonthsAndToday(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 9 * * *", Zones: []string{"Asia/Tokyo"}})
	v := res.Views[0]
	if res.From != "2026-09" || v.Start != "2026-09-01" || v.Today != 25 {
		t.Errorf("from %s start %s today %d", res.From, v.Start, v.Today)
	}
	if v.Months[0].Label != "Sep" || v.Months[11].Label != "Aug" || v.Months[4].Year != 2027 {
		t.Errorf("months %+v", v.Months)
	}
	sum := 0
	for _, m := range v.Months {
		sum += m.Count
	}
	if sum != v.Total || v.Total != 365 {
		t.Errorf("month counts sum %d, total %d", sum, v.Total)
	}
	if v.Abbr != "JST" || v.Offset != "UTC+09:00" || v.NowMinute != 21*60 {
		t.Errorf("abbr %s offset %s now %d", v.Abbr, v.Offset, v.NowMinute)
	}
}

// The hand-written encoder must agree with encoding/json.
func TestJSONMatchesEncodingJSON(t *testing.T) {
	reqs := []Request{
		{Expr: "30 2 * * *", Zones: []string{"Australia/Sydney", "Europe/London", "America/New_York"}},
		{Expr: "*/15 9-17 * * 1-5", Zones: []string{"Asia/Kolkata", "UTC"}, Mode: "each"},
		{Expr: "0 0 30 2 *", Zones: []string{"UTC"}},
		{Expr: "0 0 * * 7", Zones: []string{"UTC"}},
		{Expr: "0 0 * * *", Zones: []string{"Nowhere/<\"quoted\">"}},
	}
	for _, q := range reqs {
		q.Now = testNow
		res := Compute(q)
		var mine, std any
		if err := json.Unmarshal(res.JSON(), &mine); err != nil {
			t.Fatalf("%s: our JSON doesn't parse: %v", q.Expr, err)
		}
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, &std); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(mine, std) {
			t.Errorf("%s: encoders disagree", q.Expr)
		}
	}
}

// Go's ZoneBounds misreports the period around 31 December of a leap year
// when the zone's rule string is in force (it says the period ends at 00:00
// UTC that day, before the time asked about). periodAt must correct it, or
// walking clock changes through 2028 never ends.
func TestZoneBoundsLeapYearEdge(t *testing.T) {
	for _, z := range []string{"Europe/Paris", "America/New_York", "Australia/Sydney"} {
		loc, _ := LoadZone(z)
		for _, at := range []string{"2028-12-31T00:00:00Z", "2028-12-31T12:00:00Z", "2032-12-31T23:59:59Z"} {
			u := utc(at)
			if p := periodAt(loc, u); p.end <= u || p.start > u {
				t.Errorf("%s at %s: period [%d, %d) doesn't contain it", z, at, p.start, p.end)
			}
		}
		a, b := utc("2028-01-01T00:00:00Z"), utc("2029-01-01T00:00:00Z")
		if n := len(clockChanges(loc, a, b)); n != 2 {
			t.Errorf("%s: %d clock changes in 2028, want 2", z, n)
		}
	}
	res, _ := mustCompute(t, Request{Expr: "0 0 * * *", Zones: []string{"Europe/Paris"}, From: "2028-06"})
	if v := res.Views[0]; v.Days != 365 || v.Total != 365 {
		t.Errorf("Jun 2028 – May 2029: %d days, %d firings", v.Days, v.Total)
	}
}

// The page iterates these lists without null checks.
func TestListsAreNeverNull(t *testing.T) {
	res, _ := mustCompute(t, Request{Expr: "0 12 29 2 *", Zones: []string{"UTC", "Asia/Kolkata"}, From: "2026-09"})
	for _, v := range res.Views {
		if v.Seams == nil || v.Next == nil || v.Dial == nil || v.Months == nil || v.Bins == nil || v.Counts == nil || v.Times == nil {
			t.Errorf("%s: a list is nil: %+v", v.Zone, v)
		}
	}
	b := string(res.JSON())
	if strings.Contains(b, "null") {
		t.Errorf("JSON contains null: %.200s", b)
	}
}
