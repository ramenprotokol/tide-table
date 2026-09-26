package almanac

import (
	"math"
	"time"

	"github.com/robfig/cron/v3"
)

// Walking a year of firings.
//
// The reference is the plainest possible use of the library: the real zone,
// and Next asked for the firing after each firing it returned. walkWith does
// exactly that with any next function, and the almanac passes it fastNext,
// which returns what the real zone's Next returns for the same argument (see
// fastNext.next for why), only faster. walk_test.go compares the two across
// every zone in the embedded tz database.
//
// One thing the plain loop can't do is finish: in a few places robfig/cron's
// Next returns a time that is not later than the one it was given (in
// Pacific/Chatham, for example, Next of 02:50 on the day clocks go back is
// 02:50 again). A scheduler that keeps calling Next then re-fires in a tight
// loop. walkWith records such a loop once, as a quirk, and resumes from the
// next wall-clock minute until Next moves on.

// maxWalk is a backstop on firings per walk. A five-field cron fires at most
// once a minute, and a walk spans one year plus a few days, so it is never
// reached in practice; if it ever is, the result says the walk was cut short.
var maxWalk = 1440 * 370

// maxLoopProbes bounds the minute-by-minute search past a loop (one day).
const maxLoopProbes = 1440

// nextFunc is SpecSchedule.Next on unix seconds. ok is false when Next returns
// the zero time (nothing within its five-year search).
type nextFunc func(u int64) (next int64, ok bool)

// loop is a place where Next returned a time no later than the firing it was
// asked to follow.
type loop struct {
	at    int64 // the firing Next kept returning
	until int64 // the first minute from which Next moved on
	stuck bool  // Next didn't move on within maxLoopProbes minutes
}

type walkOut struct {
	firings   []int64
	loops     []loop
	truncated bool // stopped by maxWalk or a stuck loop
}

// step asks for the firing after prev, as a scheduler that keeps calling Next
// sees it. If Next returns a time no later than prev, it resumes from the
// next wall-clock minute, one minute at a time, until Next returns a time
// later than the minute it was asked about.
func step(next nextFunc, prev int64) (int64, bool, *loop) {
	n, ok := next(prev)
	if !ok || n > prev {
		return n, ok, nil
	}
	probe := prev
	for i := 0; i < maxLoopProbes; i++ {
		probe += 60
		n, ok = next(probe)
		if !ok || n > probe {
			return n, ok, &loop{at: prev, until: probe}
		}
	}
	return 0, false, &loop{at: prev, until: probe, stuck: true}
}

// walkWith lists every firing in [a, b).
func walkWith(next nextFunc, a, b int64) walkOut {
	w := walkOut{firings: []int64{}}
	u, ok, lp := step(next, a-1)
	if lp != nil && lp.stuck {
		w.truncated = true
	}
	for ok && u < b {
		if len(w.firings) >= maxWalk {
			w.truncated = true
			break
		}
		w.firings = append(w.firings, u)
		u, ok, lp = step(next, u)
		if lp != nil {
			w.loops = append(w.loops, *lp)
			if lp.stuck {
				w.truncated = true
			}
		}
	}
	return w
}

// libraryNext is the library's Next with the real zone.
func libraryNext(spec cron.SpecSchedule, loc *time.Location) nextFunc {
	spec.Location = loc
	return func(u int64) (int64, bool) {
		t := spec.Next(time.Unix(u, 0))
		if t.IsZero() {
			return 0, false
		}
		return t.Unix(), true
	}
}

// fastNext returns what libraryNext returns, without paying for the real
// zone where it can prove the answer is the same.
//
// Why it is needed: Go's embedded tz data is "slim", so for dates past the
// table (2037 on) every offset lookup re-derives the zone's rule, and Next
// makes dozens of lookups per call.
//
// What it does: for a call Next(u) it takes the stretch [s, e) of constant
// offset that Go's own lookup reports around u, and asks Next with a
// fixed-offset zone of that offset instead. It keeps that answer r only if
//
//  1. u reads, on that offset, as a time the expression names (so Next makes
//     no backward reset: it steps from u+1s straight to the next matching
//     second, and every intermediate time lies in [u, r]); and
//  2. u - m >= s and r + m < e, where m = |offset| + 1 hour.
//
// Then every time Next visits lies in [u, r], at least m inside the stretch.
// On such times the real zone and the fixed zone agree: the same offset, so
// the same wall-clock fields for Month, Day, Hour and the rest; the same
// instants for Add and Truncate; and the same result from time.Date, which
// looks the offset up at the wall time read as UTC (within |offset| of an
// instant inside the stretch) and finds the stretch's own offset. So the
// fixed zone's answer is the real zone's answer. Otherwise the call is made
// again with the real zone.
type fastNext struct {
	real, fixed cron.SpecSchedule
	loc         *time.Location
	s, e        int64 // the stretch around the last lookup; e <= s when none
	off         int
	zones       map[int]*time.Location
	realCalls   int
	fastCalls   int
}

func newFastNext(spec cron.SpecSchedule, loc *time.Location) *fastNext {
	f := &fastNext{real: spec, fixed: spec, loc: loc, zones: map[int]*time.Location{}}
	f.real.Location = loc
	return f
}

// stretch looks up the constant-offset stretch containing u.
func (f *fastNext) stretch(u int64) bool {
	if u >= f.s && u < f.e {
		return true
	}
	t := time.Unix(u, 0).In(f.loc)
	_, off := t.Zone()
	st, en := t.ZoneBounds()
	s, e := int64(math.MinInt64), int64(math.MaxInt64)
	if !st.IsZero() {
		s = st.Unix()
	}
	if !en.IsZero() {
		e = en.Unix()
	}
	if u < s || u >= e {
		// Go's rule-based bounds miss the last day of a leap year (see
		// periodAt). Don't use them.
		f.s, f.e = 0, 0
		return false
	}
	f.s, f.e, f.off = s, e, off
	z := f.zones[off]
	if z == nil {
		z = time.FixedZone("", off)
		f.zones[off] = z
	}
	f.fixed.Location = z
	return true
}

func (f *fastNext) next(u int64) (int64, bool) {
	if f.stretch(u) {
		m := int64(f.off)
		if m < 0 {
			m = -m
		}
		m += 3600
		if f.s <= u-m && matchesAt(&f.fixed, time.Unix(u, 0).In(f.fixed.Location)) {
			r := f.fixed.Next(time.Unix(u, 0))
			if !r.IsZero() && r.Unix() < f.e-m {
				f.fastCalls++
				return r.Unix(), true
			}
		}
	}
	f.realCalls++
	r := f.real.Next(time.Unix(u, 0))
	if r.IsZero() {
		return 0, false
	}
	return r.Unix(), true
}

// matchesAt mirrors the checks at the top of robfig/cron's Next: t matches
// the month, day, hour, minute and second fields.
func matchesAt(s *cron.SpecSchedule, t time.Time) bool {
	return 1<<uint(t.Month())&s.Month != 0 &&
		dayMatchesAt(s, t) &&
		1<<uint(t.Hour())&s.Hour != 0 &&
		1<<uint(t.Minute())&s.Minute != 0 &&
		1<<uint(t.Second())&s.Second != 0
}

// dayMatchesAt is robfig/cron's dayMatches.
func dayMatchesAt(s *cron.SpecSchedule, t time.Time) bool {
	dom := 1<<uint(t.Day())&s.Dom > 0
	dow := 1<<uint(t.Weekday())&s.Dow > 0
	if s.Dom&starBit > 0 || s.Dow&starBit > 0 {
		return dom && dow
	}
	return dom || dow
}
