package almanac

import (
	"fmt"
	"sort"
	"strings"

	"github.com/robfig/cron/v3"
)

// The words on the seams. Each sentence is built from what the walk measured
// (which minutes were skipped, repeated or added) and from facts about the
// clock change (its direction and the wall-clock span it skipped or
// repeated), so a sentence never claims more than the numbers show.

// wallList names wall-clock minutes: a regular run reads as a range
// ("each minute from 02:00 to 02:59"), anything else as a list.
func wallList(mins []int) string {
	if n := len(mins); n > 4 {
		step := mins[1] - mins[0]
		even := step > 0
		for i := 2; i < n && even; i++ {
			even = mins[i]-mins[i-1] == step
		}
		if even && step == 1 {
			return fmt.Sprintf("each minute from %s to %s", fmtMinute(mins[0]), fmtMinute(mins[n-1]))
		}
		if even {
			return fmt.Sprintf("every %d minutes from %s to %s", step, fmtMinute(mins[0]), fmtMinute(mins[n-1]))
		}
	}
	var s []string
	for i, m := range mins {
		if i == maxWallList {
			s = append(s, fmt.Sprintf("%d more", len(mins)-maxWallList))
			break
		}
		s = append(s, fmtMinute(m))
	}
	return joinAnd(s)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// anomalyLabel is the short tag printed beside a seam.
func anomalyLabel(a *anomaly) string {
	one := func(mins []int, single, many string) string {
		if len(mins) == 1 {
			return fmtMinute(mins[0]) + " " + single
		}
		return fmt.Sprintf("%d %s", len(mins), many)
	}
	switch {
	case len(a.loops) > 0:
		return fmtMinute(a.loopMinute(0)) + " re-fires"
	case len(a.skipped) > 0 && len(a.extra) > 0:
		return fmt.Sprintf("%d skipped, %d extra", len(a.skipped), len(a.extra))
	case len(a.skipped) > 0:
		return one(a.skipped, "skipped", "skipped")
	case len(a.repeated) > 0:
		return one(a.repeated, "twice", "ran twice")
	case len(a.extra) > 0:
		return one(a.extra, "extra run", "extra runs")
	}
	return ""
}

// anomalyDetail lists what the library did, for sentences without a change.
func anomalyDetail(a *anomaly) string {
	var d []string
	if len(a.skipped) > 0 {
		d = append(d, "skips "+wallList(a.skipped))
	}
	if len(a.repeated) > 0 {
		d = append(d, "fires twice at "+wallList(a.repeated))
	}
	if len(a.extra) > 0 {
		d = append(d, "also fires at "+wallList(a.extra)+", which the expression doesn’t name")
	}
	for i := range a.loops {
		d = append(d, "re-fires in a tight loop at "+fmtMinute(a.loopMinute(i)))
	}
	return joinAnd(d)
}

// loopMinute is the wall minute (run zone) of the i-th loop's firing.
func (a *anomaly) loopMinute(i int) int { return a.loopWall[i] }

// loopText explains one place where Next did not move on.
func loopText(r *runState, a *anomaly, i int, same bool) string {
	lp := a.loops[i]
	where := ""
	if !same {
		where = " (" + r.name + " time)"
	}
	if lp.stuck {
		return fmt.Sprintf("After the %s run, robfig/cron would keep re-firing in a tight loop for more than a day%s. This is a library quirk; the almanac stops here.", fmtMinute(a.loopWall[i]), where)
	}
	return fmt.Sprintf("After the %s run, robfig/cron would re-fire in a tight loop until %s%s. This is a library quirk; shown once here.", fmtMinute(a.loopWall[i]), fmtMinute(wallMinute(r.loc, lp.until)), where)
}

// hourSteps lists three readings of robfig/cron's hour-by-hour search either
// side of a change: its steps are whole hours on the clock before the change,
// and they read differently after it.
func hourSteps(c change) string {
	wb := c.at + int64(c.before.off)
	s0 := floorDiv(wb-1, 3600)*3600 - int64(c.before.off)
	var r []string
	for k := int64(0); k < 3; k++ {
		u := s0 + k*3600
		off := c.before.off
		if u >= c.at {
			off = c.after.off
		}
		r = append(r, wallAt(u, off))
	}
	return strings.Join(r, ", ") + ", …"
}

// passedText explains skipped firings whose wall time does happen.
func passedText(c change, mins []int) string {
	var what, it string
	if len(mins) == 1 {
		what, it = fmtMinute(mins[0])+" does happen that day, but robfig/cron skips it", fmtMinute(mins[0])
	} else {
		what, it = fmt.Sprintf("robfig/cron also skips %d firings whose times do happen that day (%s)", len(mins), wallList(mins)), "them"
	}
	whole := c.delta()%3600 == 0
	onHour := floorMod(c.at+int64(c.before.off), 3600) == 0
	if whole && onHour {
		return what + ": its search passes over " + it + " around this change. This is a library quirk, not a missing time."
	}
	size := ""
	if !whole {
		size = " of " + fmtDuration(c.delta())
	}
	return fmt.Sprintf("%s: its search moves an hour at a time (%s), so after this change%s it never looks at %s. This is a library quirk, not a missing time.", what, hourSteps(c), size, it)
}

// runChangeText explains a clock change on the zone cron runs in.
func runChangeText(r *runState, c change, a *anomaly, same bool) string {
	lo, hi := c.wallSpan()
	span := fmt.Sprintf("%s–%s", fmtMinute(int(floorMod(lo, 86400)/60)), fmtMinute(int(floorMod(hi-60, 86400)/60)))
	from, to := wallAt(c.at, c.before.off), wallAt(c.at, c.after.off)
	why := "the zone’s standard offset changes"
	switch {
	case c.after.dst && !c.before.dst:
		why = "daylight saving begins"
	case !c.after.dst && c.before.dst:
		why = "daylight saving ends"
	}
	who := "Clocks"
	if !same {
		who = fmt.Sprintf("In %s, where this schedule runs, clocks", r.name)
	}
	head := fmt.Sprintf("%s %s %s at %s, to %s (%s becomes %s; %s).", who, goWord(c.delta()), fmtDuration(c.delta()), from, to, c.before.abbr, c.after.abbr, why)

	forward := c.delta() > 0
	if a == nil {
		if forward {
			return head + fmt.Sprintf(" No firing falls in the missing %s.", span)
		}
		return head + fmt.Sprintf(" No firing falls in the repeated %s.", span)
	}
	inSpan := func(w int64) bool { return w >= lo && w < hi }
	parts := []string{head}

	// Skipped: "doesn't happen" only when the time is in a forward gap.
	var gap, passed []int
	for i, m := range a.skipped {
		if forward && inSpan(a.skipDay[i]*86400+int64(m)*60) {
			gap = append(gap, m)
		} else {
			passed = append(passed, m)
		}
	}
	switch len(gap) {
	case 0:
	case 1:
		parts = append(parts, fmt.Sprintf("%s doesn’t happen, so robfig/cron skips the %s firing.", span, fmtMinute(gap[0])))
	default:
		parts = append(parts, fmt.Sprintf("%s doesn’t happen, so robfig/cron skips %d firings (%s).", span, len(gap), wallList(gap)))
	}
	if len(passed) > 0 {
		parts = append(parts, passedText(c, passed))
	}

	// Repeated: "happens twice" only when the time is in a backward overlap.
	var twice, odd []int
	for i, m := range a.repeated {
		u := a.repInst[i][0]
		if !forward && inSpan(u+int64(periodAt(r.loc, u).off)) {
			twice = append(twice, m)
		} else {
			odd = append(odd, m)
		}
	}
	switch len(twice) {
	case 0:
	case 1:
		parts = append(parts, fmt.Sprintf("%s happens twice, and robfig/cron fires at %s both times, %s apart.", span, fmtMinute(twice[0]), fmtDuration(c.delta())))
	default:
		parts = append(parts, fmt.Sprintf("%s happens twice, and robfig/cron fires both times at %s.", span, wallList(twice)))
	}
	if len(odd) > 0 {
		parts = append(parts, fmt.Sprintf("robfig/cron fires twice at %s, which %s only once that day. This is a library quirk.", wallList(odd), plural(len(odd), "happens", "happen")))
	}

	// Extra runs at times the expression doesn't name.
	if len(a.extra) > 0 {
		s := fmt.Sprintf("robfig/cron also fires at %s, which the expression doesn’t name: %s.", wallList(a.extra), plural(len(a.extra), "an extra run", "extra runs"))
		if a.extraByMinuteSearch(&r.spec) {
			s += " Its search steps minute by minute across the change and stops at the first matching minute without checking the hour again. This is a library quirk."
		} else {
			s += " This is a library quirk."
		}
		parts = append(parts, s)
	}
	for i := range a.loops {
		parts = append(parts, loopText(r, a, i, same))
	}
	return strings.Join(parts, " ")
}

// extraByMinuteSearch: every extra minute names a minute the expression has,
// in an hour it doesn't.
func (a *anomaly) extraByMinuteSearch(s *cron.SpecSchedule) bool {
	for _, m := range a.extra {
		if s.Minute&(1<<uint(m%60)) == 0 || s.Hour&(1<<uint(m/60)) != 0 {
			return false
		}
	}
	return len(a.extra) > 0
}

// searchOrigin is where robfig/cron's search for a day's runs began: the last
// firing before the day, or the start of the walk. ok is false for the
// start of the walk.
func searchOrigin(r *runState, day int64) (int64, bool) {
	dayStart := startOfDayNum(r.loc, day)
	i := sort.Search(len(r.firings), func(i int) bool { return r.firings[i] >= dayStart })
	if i == 0 {
		return r.a, false
	}
	return r.firings[i-1], true
}

// carriedText explains differences on consecutive days no clock change
// touches, which share one cause (group[0] is the first day).
func carriedText(r *runState, group []*anomaly, same bool) string {
	a := group[0]
	where := ""
	if !same {
		where = " in " + r.name
	}
	var head string
	if len(group) == 1 {
		head = fmt.Sprintf("On %s, robfig/cron%s %s. No clocks change that day.", fmtDate(a.day), where, anomalyDetail(a))
	} else {
		head = fmt.Sprintf("From %s to %s (%d days), robfig/cron%s %s each day. No clocks change on those days.", fmtDate(a.day), fmtDate(group[len(group)-1].day), len(group), where, anomalyDetail(a))
	}
	var tail string
	for _, g := range group {
		for k := range g.loops {
			tail += " " + loopText(r, g, k, same)
		}
	}
	origin, isRun := searchOrigin(r, a.day)
	dayStart := startOfDayNum(r.loc, a.day)
	var c *change
	for k := range r.changes {
		if r.changes[k].at > origin && r.changes[k].at < dayStart {
			c = &r.changes[k]
		}
	}
	if c == nil {
		return head + " No clock change explains it; this almanac shows it as the library returned it." + tail
	}
	p := periodAt(r.loc, origin)
	from := "the start of this almanac"
	if isRun {
		from = "the previous run"
	}
	runs := "this run"
	if len(group) > 1 {
		runs = "these runs"
	}
	return head + fmt.Sprintf(" robfig/cron looked for %s by searching on from %s, %s at %s, across the clock change on %s (%s becomes %s), and that change threw its search off. This is a library quirk, not a missing time.%s",
		runs, from, fmtDate(civilDay(origin+int64(p.off))), wallAt(origin, p.off), fmtDate(civilDay(c.at+int64(c.after.off))), c.before.abbr, c.after.abbr, tail)
}

// sameCarried reports whether two carried anomalies belong in one seam:
// consecutive days, the same difference, and the same search behind them.
func sameCarried(r *runState, x, y *anomaly) bool {
	if y.day != x.day+1 || len(x.loops)+len(y.loops) > 0 || anomalyDetail(x) != anomalyDetail(y) {
		return false
	}
	ox, _ := searchOrigin(r, x.day)
	oy, _ := searchOrigin(r, y.day)
	return ox == oy
}

// anomalyMarks puts an anomaly's affected firings on the view's clock.
func (v *viewState) anomalyMarks(a *anomaly, same bool, day, mnt int, localOf func(int64) (int, int), addMark func(int, int, string)) {
	r := v.run
	for i, w := range a.skipped {
		if i >= maxWallList {
			break
		}
		if same {
			addMark(int(a.skipDay[i]-v.civil0), w, "skip")
			continue
		}
		// On another clock, place it where that wall time would have been.
		if u, ok := instantOfWall(r, a.skipDay[i]*86400+int64(w)*60); ok {
			dd, dm := localOf(u + 1)
			addMark(dd, dm, "skip")
		} else {
			addMark(day, mnt, "skip")
		}
	}
	for i, inst := range a.repInst {
		if i >= maxWallList {
			break
		}
		for _, u := range inst {
			dd, dm := localOf(u + 1)
			addMark(dd, dm, "double")
		}
	}
	for _, u := range a.extraAt {
		dd, dm := localOf(u + 1)
		addMark(dd, dm, "extra")
	}
}

// instantOfWall finds an instant whose reading in the run zone is wall
// (seconds of local time), if the wall time exists.
func instantOfWall(r *runState, w int64) (int64, bool) {
	for _, d := range []int64{0, -86400, 86400} {
		p := periodAt(r.loc, w+d)
		u := w - int64(p.off)
		if periodAt(r.loc, u).off == p.off {
			return u, true
		}
	}
	return 0, false
}
