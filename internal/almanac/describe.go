package almanac

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/robfig/cron/v3"
)

// starBit is robfig/cron’s marker for a field written as * or ?. It decides
// whether day-of-month and day-of-week combine with AND or OR.
const starBit = uint64(1) << 63

var monthNames = [13]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var dayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// daysIn is the longest each month can be (February counts its leap day).
var daysIn = [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func members(b uint64, lo, hi int) []int {
	var out []int
	for i := lo; i <= hi; i++ {
		if b&(1<<uint(i)) != 0 {
			out = append(out, i)
		}
	}
	return out
}

func count(b uint64) int { return bits.OnesCount64(b &^ starBit) }

func isAll(b uint64, lo, hi int) bool { return len(members(b, lo, hi)) == hi-lo+1 }

// contiguous reports whether the set is one unbroken run.
func contiguous(s []int) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1]+1 {
			return false
		}
	}
	return len(s) > 0
}

// progression reports a step of at least 2 across at least 3 members.
func progression(s []int) (step int, ok bool) {
	if len(s) < 3 {
		return 0, false
	}
	step = s[1] - s[0]
	if step < 2 {
		return 0, false
	}
	for i := 2; i < len(s); i++ {
		if s[i]-s[i-1] != step {
			return 0, false
		}
	}
	return step, true
}

// runs splits a sorted set into contiguous [lo, hi] blocks.
func runs(s []int) [][2]int {
	var out [][2]int
	for i, v := range s {
		if i > 0 && v == s[i-1]+1 {
			out[len(out)-1][1] = v
			continue
		}
		out = append(out, [2]int{v, v})
	}
	return out
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func hhmm(h, m int) string { return fmt.Sprintf("%02d:%02d", h, m) }

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// Describe renders a parsed schedule as one plain-English sentence, working
// from the library's parsed bit sets so equivalent spellings read the same.
func Describe(s *cron.SpecSchedule) string {
	timePart, timeIsEvery := describeTime(s.Minute, s.Hour)
	dayPart, never := describeDays(s)
	if never {
		return timePart + ", " + dayPart
	}
	if dayPart == "every day" && timeIsEvery {
		return timePart
	}
	return timePart + ", " + dayPart
}

// describeTime returns the time-of-day phrase and whether it starts with
// "Every" (so a trailing "every day" would be redundant).
func describeTime(minBits, hourBits uint64) (string, bool) {
	mins := members(minBits, 0, 59)
	hours := members(hourBits, 0, 23)
	allMins, allHours := len(mins) == 60, len(hours) == 24

	// A handful of exact times: list them.
	if len(mins)*len(hours) <= 6 && !allMins {
		var times []string
		for _, h := range hours {
			for _, m := range mins {
				times = append(times, hhmm(h, m))
			}
		}
		return "At " + joinAnd(times), false
	}

	hourSpan := func() string { // "from 09:00 through 17:59" style, for whole hours
		var parts []string
		for _, r := range runs(hours) {
			parts = append(parts, fmt.Sprintf("from %s through %s", hhmm(r[0], 0), hhmm(r[1], 59)))
		}
		return joinAnd(parts)
	}

	switch {
	case allMins && allHours:
		return "Every minute", true
	case allMins:
		return "Every minute " + hourSpan(), true
	}

	// Minute phrase.
	minStep, minIsStep := progression(mins)
	everyN := ""
	if minIsStep && mins[0] < minStep && mins[len(mins)-1]+minStep >= 60 {
		if mins[0] == 0 {
			everyN = fmt.Sprintf("Every %d minutes", minStep)
			if 60%minStep != 0 {
				everyN += " (the count restarts at the top of each hour)"
			}
		}
	}

	if allHours {
		switch {
		case len(mins) == 1 && mins[0] == 0:
			return "Every hour, on the hour", true
		case len(mins) == 1:
			return fmt.Sprintf("Every hour at :%02d", mins[0]), true
		case everyN != "":
			return everyN, true
		}
		return "Every hour at " + minuteList(mins), true
	}

	if hs, ok := progression(hours); ok && hours[0] < hs && hours[len(hours)-1]+hs >= 24 && len(mins) == 1 {
		at := fmt.Sprintf("at :%02d", mins[0])
		if mins[0] == 0 {
			at = "on the hour"
		}
		start := ""
		if hours[0] != 0 {
			start = fmt.Sprintf(", starting at %s", hhmm(hours[0], mins[0]))
		}
		return fmt.Sprintf("Every %d hours %s%s", hs, at, start), true
	}

	if contiguous(hours) {
		first, last := hours[0], hours[len(hours)-1]
		switch {
		case len(mins) == 1 && mins[0] == 0:
			return fmt.Sprintf("Every hour on the hour from %s through %s", hhmm(first, 0), hhmm(last, 0)), true
		case len(mins) == 1:
			return fmt.Sprintf("Every hour at :%02d from %s through %s", mins[0], hhmm(first, mins[0]), hhmm(last, mins[0])), true
		case everyN != "":
			return fmt.Sprintf("%s from %s through %s", everyN, hhmm(first, mins[0]), hhmm(last, mins[len(mins)-1])), true
		}
		return fmt.Sprintf("At %s past each hour from %s through %s", minuteList(mins), hhmm(first, mins[0]), hhmm(last, mins[len(mins)-1])), false
	}

	var hl []string
	for _, h := range hours {
		hl = append(hl, fmt.Sprintf("%02d", h))
	}
	if everyN != "" {
		return fmt.Sprintf("%s during hours %s", everyN, joinAnd(hl)), true
	}
	return fmt.Sprintf("At %s past hours %s", minuteList(mins), joinAnd(hl)), false
}

func minuteList(mins []int) string {
	if len(mins) > 8 {
		return fmt.Sprintf("%d set minutes (:%02d to :%02d)", len(mins), mins[0], mins[len(mins)-1])
	}
	var ml []string
	for _, m := range mins {
		ml = append(ml, fmt.Sprintf(":%02d", m))
	}
	return joinAnd(ml)
}

// describeDays covers day of month, month and day of week, following
// robfig/cron’s dayMatches: if either day field was written as * (or ?), both
// must match; if neither was, a date matches when EITHER does. never is true
// when no date can match (like 30 February).
func describeDays(s *cron.SpecSchedule) (string, bool) {
	doms := members(s.Dom, 1, 31)
	months := members(s.Month, 1, 12)
	dows := members(s.Dow, 0, 6)
	allMonths := len(months) == 12
	allDoms := len(doms) == 31
	allDows := len(dows) == 7
	and := s.Dom&starBit != 0 || s.Dow&starBit != 0

	everyDay := func() string {
		if allMonths {
			return "every day"
		}
		return "every day " + inMonths(months)
	}
	weekdays := func() string {
		p := describeDows(dows)
		if !allMonths {
			p += ", " + describeMonths(months)
		}
		return p
	}

	if and {
		switch {
		case allDoms && allDows:
			return everyDay(), false
		case allDoms:
			return weekdays(), false
		case !allDows:
			// Not reachable from the parser (a star sets every bit), kept for safety.
			return describeDows(dows) + " that fall on " + domOfMonths(doms, months), false
		}
		if !anyDateExists(doms, months) {
			return "on " + domOfMonths(doms, months) + ", a date that never occurs, so this never fires", true
		}
		p := "on " + domOfMonths(doms, months)
		if n := missingNote(doms, months); n != "" {
			p += " " + n
		}
		return p, false
	}

	// Neither day field is a star: cron fires when EITHER matches.
	if allDoms || allDows {
		return everyDay() + " (with both day fields set, cron matches either, and one covers every day)", false
	}
	p := fmt.Sprintf("on %s, and also %s", domOfMonths(doms, allMonthList()), strings.TrimPrefix(describeDows(dows), "on "))
	if !allMonths {
		p += ", " + describeMonths(months)
	}
	return p + " (cron matches either day rule)", false
}

func allMonthList() []int {
	all := make([]int, 12)
	for i := range all {
		all[i] = i + 1
	}
	return all
}

func describeDows(dows []int) string {
	switch {
	case len(dows) == 1:
		return "every " + dayNames[dows[0]]
	case len(dows) == 2 && dows[0] == 0 && dows[1] == 6:
		return "on Saturday and Sunday"
	case len(dows) >= 3 && contiguous(dows):
		return fmt.Sprintf("%s through %s", dayNames[dows[0]], dayNames[dows[len(dows)-1]])
	}
	var names []string
	for _, d := range dows {
		names = append(names, dayNames[d])
	}
	return "on " + joinAnd(names)
}

func inMonths(months []int) string {
	if len(months) >= 3 && contiguous(months) {
		return fmt.Sprintf("from %s through %s", monthNames[months[0]], monthNames[months[len(months)-1]])
	}
	var names []string
	for _, m := range months {
		names = append(names, monthNames[m])
	}
	return "in " + joinAnd(names)
}

func describeMonths(months []int) string {
	if len(months) >= 3 && contiguous(months) {
		return fmt.Sprintf("%s through %s", monthNames[months[0]], monthNames[months[len(months)-1]])
	}
	return "in " + strings.TrimPrefix(inMonths(months), "in ")
}

// domOfMonths: "the 1st of every month", "1 January", "the 1st and 15th of
// March and June", "days 1 through 7 of every month".
func domOfMonths(doms, months []int) string {
	allMonths := len(months) == 12
	var dayText string
	switch {
	case len(doms) >= 3 && contiguous(doms):
		dayText = fmt.Sprintf("the %s through %s", ordinal(doms[0]), ordinal(doms[len(doms)-1]))
	case len(doms) > 6:
		if step, ok := progression(doms); ok {
			dayText = fmt.Sprintf("every %s day from the %s", ordinal(step), ordinal(doms[0]))
		} else {
			dayText = fmt.Sprintf("%d chosen days", len(doms))
		}
	default:
		var ds []string
		for _, d := range doms {
			ds = append(ds, ordinal(d))
		}
		dayText = "the " + joinAnd(ds)
	}
	if allMonths {
		return dayText + " of every month"
	}
	if len(doms) == 1 && len(months) == 1 {
		return fmt.Sprintf("%d %s", doms[0], monthNames[months[0]])
	}
	return dayText + " of " + strings.TrimPrefix(inMonths(months), "in ")
}

func anyDateExists(doms, months []int) bool {
	for _, m := range months {
		for _, d := range doms {
			if d <= daysIn[m] {
				return true
			}
		}
	}
	return false
}

// missingNote explains days that some chosen months don’t have.
func missingNote(doms, months []int) string {
	if len(doms) == 1 && doms[0] == 29 && len(months) == 1 && months[0] == 2 {
		return "(leap years only)"
	}
	maxDom := doms[len(doms)-1]
	if doms[0] < 29 || len(doms) != 1 {
		return ""
	}
	short := 0
	for _, m := range months {
		if daysIn[m] < maxDom || m == 2 {
			short++
		}
	}
	if short == 0 {
		return ""
	}
	return fmt.Sprintf("(skipped in months without a %s)", ordinal(maxDom))
}
