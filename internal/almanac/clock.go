package almanac

import (
	"fmt"
	"math"
	"time"
)

// period is one stretch of constant UTC offset in a zone.
type period struct {
	start, end int64 // unix seconds; MinInt64/MaxInt64 when unbounded
	off        int   // seconds east of UTC
	abbr       string
	dst        bool
}

func periodAt(loc *time.Location, u int64) period {
	t := time.Unix(u, 0).In(loc)
	abbr, off := t.Zone()
	s, e := t.ZoneBounds()
	p := period{start: math.MinInt64, end: math.MaxInt64, off: off, abbr: abbr, dst: t.IsDST()}
	if !s.IsZero() {
		p.start = s.Unix()
	}
	if !e.IsZero() {
		p.end = e.Unix()
	}
	if p.start > u {
		p.start = u
	}
	if p.end <= u {
		// Go's Time.ZoneBounds (seen in Go 1.26 with the slim tz data that
		// time/tzdata embeds) reports a rule-based period that contains
		// 31 December of a leap year as ending at 00:00 UTC that day, which
		// is before u. That year edge isn't a clock change (no current rule
		// changes clocks on 31 December), so look a day ahead for the end.
		p.end = u + 86400
		if q := periodAt(loc, p.end); q.off == p.off && q.end > p.end {
			p.end = q.end
		}
	}
	return p
}

// cursor answers offset lookups for mostly-ascending instants cheaply.
type cursor struct {
	loc *time.Location
	p   period
	ok  bool
}

func (c *cursor) at(u int64) period {
	if !c.ok || u < c.p.start || u >= c.p.end {
		c.p = periodAt(c.loc, u)
		c.ok = true
	}
	return c.p
}

// change is a moment a zone's UTC offset changed.
type change struct {
	at            int64
	before, after period
}

func (c change) delta() int { return c.after.off - c.before.off }

// wallSpan is the stretch of local wall-clock seconds the change skipped
// (clocks forward) or repeated (clocks back): [lo, hi).
func (c change) wallSpan() (lo, hi int64) {
	a, b := int64(c.before.off), int64(c.after.off)
	return c.at + min(a, b), c.at + max(a, b)
}

// clockChanges lists offset changes in [a, b). Transitions that only rename a
// zone or flip its DST flag without moving the clock are ignored.
func clockChanges(loc *time.Location, a, b int64) []change {
	var out []change
	u := a
	for {
		p := periodAt(loc, u)
		if p.end == math.MaxInt64 || p.end >= b {
			return out
		}
		q := periodAt(loc, p.end)
		if q.off != p.off {
			out = append(out, change{at: p.end, before: p, after: q})
		}
		u = p.end
	}
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

// civilDay numbers local calendar days (days since 1970-01-01 on the wall
// clock). local is unix seconds plus the zone offset.
func civilDay(local int64) int64 { return floorDiv(local, 86400) }

func civilDate(day int64) (int, time.Month, int) {
	return time.Unix(day*86400, 0).UTC().Date()
}

func dayNumber(y int, m time.Month, d int) int64 {
	return floorDiv(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix(), 86400)
}

// startOfDay is the first instant whose wall-clock date is y-m-d in loc. Where
// clocks jump over midnight, 00:00 doesn't exist and the day starts at the jump.
func startOfDay(loc *time.Location, y int, m time.Month, d int) int64 {
	want := dayNumber(y, m, d) * 86400 // local midnight, in wall-clock seconds
	g := time.Date(y, m, d, 0, 0, 0, 0, loc).Unix()
	p0 := periodAt(loc, g)
	ps := []period{p0}
	if p0.start != math.MinInt64 {
		ps = append([]period{periodAt(loc, p0.start-1)}, ps...)
	}
	if p0.end != math.MaxInt64 {
		ps = append(ps, periodAt(loc, p0.end))
	}
	best := int64(math.MaxInt64)
	for _, p := range ps {
		if t := want - int64(p.off); t >= p.start && t < p.end && t < best {
			best = t
		}
	}
	if best != math.MaxInt64 {
		return best
	}
	// Midnight fell in a forward jump: the day begins at the jump.
	for i := 0; i+1 < len(ps); i++ {
		b := ps[i].end
		if want >= b+int64(ps[i].off) && want < b+int64(ps[i+1].off) {
			return b
		}
	}
	return g
}

// Formatting helpers.

func fmtOffset(off int) string {
	sign := "+"
	if off < 0 {
		sign = "−"
		off = -off
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, off/3600, off%3600/60)
}

func fmtZone(p period) string { return fmt.Sprintf("%s, %s", p.abbr, fmtOffset(p.off)) }

var shortDays = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var shortMonths = [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func fmtDate(day int64) string {
	y, m, d := civilDate(day)
	wd := time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Weekday()
	return fmt.Sprintf("%s %d %s %d", shortDays[wd], d, shortMonths[m], y)
}

func fmtISO(day int64) string {
	y, m, d := civilDate(day)
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), d)
}

func fmtMinute(m int) string { return hhmm(m/60, m%60) }

func fmtDuration(sec int) string {
	if sec < 0 {
		sec = -sec
	}
	h, m := sec/3600, sec%3600/60
	switch {
	case m == 0 && h == 1:
		return "an hour"
	case m == 0:
		return fmt.Sprintf("%d hours", h)
	case h == 0:
		return fmt.Sprintf("%d minutes", m)
	}
	return fmt.Sprintf("%d h %02d min", h, m)
}
