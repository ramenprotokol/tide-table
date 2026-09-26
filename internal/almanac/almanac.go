package almanac

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Limits. Work is bounded by construction: at most MaxZones zones, one
// 12-month window each, and a five-field cron fires at most once a minute
// (527,040 times in a leap year). maxWalk is a backstop above that.
const (
	MaxZones    = 3
	NextCount   = 10
	BinMinutes  = 15
	BinsPerDay  = 24 * 60 / BinMinutes
	MinYear     = 1970
	MaxYear     = 2199
	maxZoneLen  = 64
	maxMarks    = 48
	maxWallList = 12
	maxTimes    = 4
	maxWalk     = 1440 * 370
)

// walkStride is how far past each firing the walk asks Next to start. Five-
// field cron only fires at second 0, so starting 59 s later returns the same
// next firing while skipping the library's second-by-second scan. A test walks
// with a stride of 0 and checks the results are identical.
var walkStride = 59 * time.Second

// Request is one almanac query.
type Request struct {
	Expr  string   `json:"expr"`
	Zones []string `json:"zones"`
	Mode  string   `json:"mode"` // "shared": cron runs on zones[0]'s clock; "each": on each zone’s own
	From  string   `json:"from"` // first month of the almanac, "YYYY-MM"; "" means the current month
	Now   int64    `json:"now"`  // unix milliseconds; 0 means the current time
}

// InputError explains which input is wrong.
type InputError struct {
	Kind    string `json:"kind"`  // "expr", "zone" or "from"
	Field   int    `json:"field"` // cron field 0–4 for expr errors, else -1
	Index   int    `json:"index"` // zone index for zone errors, else -1
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
}

func (e *InputError) Error() string { return e.Message }

// Result is everything the page draws.
type Result struct {
	Error       *InputError `json:"error,omitempty"`
	Expr        string      `json:"expr"`
	Fields      [5]string   `json:"fields"`
	Description string      `json:"description"`
	Mode        string      `json:"mode"`
	From        string      `json:"from"`
	PerDay      int         `json:"perDay"` // firings on a matching day with no clock change
	Never       bool        `json:"never"`
	Walked      int         `json:"walked"` // firings walked with the library's Next
	Views       []View      `json:"views"`
}

// View is one zone’s page of the almanac.
type View struct {
	Zone      string   `json:"zone"`
	RunZone   string   `json:"runZone"`
	Start     string   `json:"start"` // local date of day 0
	Days      int      `json:"days"`
	Today     int      `json:"today"`     // day index of now, or -1
	NowMinute int      `json:"nowMinute"` // local minute of day now
	Abbr      string   `json:"abbr"`
	Offset    string   `json:"offset"`
	Months    []Month  `json:"months"`
	Bins      []string `json:"bins"` // per day: 24 hex digits, digit k holds 15-minute bins 4k..4k+3 (bit j = bin 4k+j); "" if none
	Counts    []int    `json:"counts"`
	Times     []string `json:"times"` // per day: the local times when it fires at most 4 times, else ""
	Dial      [][2]int `json:"dial"`  // [local minute of day, firings at that minute]
	Seams     []Seam   `json:"seams"`
	Total     int      `json:"total"`
	Next      []Firing `json:"next"`
	NextNote  string   `json:"nextNote,omitempty"`
}

// Month is one column of the year strip.
type Month struct {
	Year  int    `json:"year"`
	Month int    `json:"month"`
	Label string `json:"label"`
	First int    `json:"first"` // day index of the 1st
	Days  int    `json:"days"`
	Count int    `json:"count"`
}

// Mark is a firing a seam affected, placed on the view's clock: where a
// skipped firing would have been, or each run of a repeated one.
type Mark struct {
	Day    int    `json:"day"`
	Minute int    `json:"minute"`
	Kind   string `json:"kind"` // skip, double, moved
}

// Seam is a clock change drawn across the year strip.
type Seam struct {
	Kind   string `json:"kind"` // skip, double, moved, shift, quiet
	Red    bool   `json:"red"`
	Day    int    `json:"day"`
	Minute int    `json:"minute"`
	At     int64  `json:"at"` // unix ms
	Date   string `json:"date"`
	Time   string `json:"time"`
	Zone   string `json:"zone"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Marks  []Mark `json:"marks"`
}

// Firing is one upcoming run, read on a view's clock.
type Firing struct {
	At     int64  `json:"at"`
	Date   string `json:"date"`
	Time   string `json:"time"`
	Abbr   string `json:"abbr"`
	Offset string `json:"offset"`
	Note   string `json:"note,omitempty"`
}

func validZoneName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '-' || c == '/'
		if !ok {
			return false
		}
	}
	return s != "" && !strings.Contains(s, "..")
}

// parseMonth reads "YYYY-MM".
func parseMonth(s string) (y, m int, ok bool) {
	if len(s) != 7 || s[4] != '-' {
		return 0, 0, false
	}
	for i, c := range []byte(s) {
		if i != 4 && (c < '0' || c > '9') {
			return 0, 0, false
		}
	}
	y, _ = strconv.Atoi(s[:4])
	m, _ = strconv.Atoi(s[5:])
	return y, m, true
}

// LoadZone validates an IANA zone name against Go's tz database.
func LoadZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return nil, fmt.Errorf("Choose a time zone, for example Europe/London.")
	case len(name) > maxZoneLen:
		return nil, fmt.Errorf("That zone name is too long.")
	case name == "Local":
		return nil, fmt.Errorf("“Local” depends on the machine. Name a zone such as Europe/London.")
	case !validZoneName(name):
		return nil, fmt.Errorf("“%s” isn’t an IANA zone name. Those look like Area/City, for example Asia/Tokyo.", name)
	}
	if !knownZone(name) {
		if s := suggestZone(name); s != "" {
			return nil, fmt.Errorf("Unknown time zone “%s”. Did you mean %s? Zone names are case-sensitive.", name, s)
		}
		return nil, fmt.Errorf("Unknown time zone “%s”. Use an IANA name such as America/New_York.", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("Couldn’t load the time zone “%s”.", name)
	}
	return loc, nil
}

type runState struct {
	name     string
	loc      *time.Location
	spec     cron.SpecSchedule
	a, b     int64 // walk range
	firings  []int64
	anoms    []*anomaly
	changes  []change
	next     []int64
	nextNote []string
}

type viewState struct {
	zone   string
	loc    *time.Location
	run    *runState
	ws, we int64
	civil0 int64
	days   int
}

// Compute builds the almanac.
func Compute(req Request) *Result {
	res, _ := compute(req)
	return res
}

// compute also returns the run states, for tests.
func compute(req Request) (*Result, map[string]*runState) {
	res := &Result{Mode: req.Mode}
	fail := func(e *InputError) (*Result, map[string]*runState) { res.Error = e; return res, nil }

	sched, err := Parse(req.Expr)
	if err != nil {
		pe := err.(*ParseError)
		return fail(&InputError{Kind: "expr", Field: pe.Field, Index: -1, Token: pe.Token, Message: pe.Message})
	}
	res.Expr = strings.Join(sched.Fields[:], " ")
	res.Fields = sched.Fields
	res.Description = Describe(sched.Spec)
	res.PerDay = count(sched.Spec.Hour) * count(sched.Spec.Minute)

	if res.Mode == "" {
		res.Mode = "shared"
	}
	if res.Mode != "shared" && res.Mode != "each" {
		return fail(&InputError{Kind: "zone", Field: -1, Index: -1, Message: "Mode must be “shared” or “each”."})
	}
	if len(req.Zones) == 0 {
		return fail(&InputError{Kind: "zone", Field: -1, Index: 0, Message: "Choose at least one time zone."})
	}
	if len(req.Zones) > MaxZones {
		return fail(&InputError{Kind: "zone", Field: -1, Index: MaxZones, Message: fmt.Sprintf("At most %d time zones at once.", MaxZones)})
	}
	locs := make([]*time.Location, len(req.Zones))
	names := make([]string, len(req.Zones))
	seen := map[string]int{}
	for i, z := range req.Zones {
		loc, err := LoadZone(z)
		if err != nil {
			return fail(&InputError{Kind: "zone", Field: -1, Index: i, Token: z, Message: err.Error()})
		}
		names[i] = strings.TrimSpace(z)
		if j, dup := seen[names[i]]; dup {
			return fail(&InputError{Kind: "zone", Field: -1, Index: i, Token: z, Message: fmt.Sprintf("%s is already zone %d.", names[i], j+1)})
		}
		seen[names[i]] = i
		locs[i] = loc
	}

	now := req.Now / 1000
	if req.Now == 0 {
		now = time.Now().Unix()
	}
	var fy int
	var fm time.Month
	if req.From == "" {
		fy, fm, _ = time.Unix(now, 0).In(locs[0]).Date()
	} else {
		y, mm, ok := parseMonth(strings.TrimSpace(req.From))
		if !ok {
			return fail(&InputError{Kind: "from", Field: -1, Index: -1, Token: req.From, Message: "The start month should look like 2027-01."})
		}
		fy = y
		if mm < 1 || mm > 12 {
			return fail(&InputError{Kind: "from", Field: -1, Index: -1, Token: req.From, Message: "The month must be 01 to 12."})
		}
		fm = time.Month(mm)
	}
	if fy < MinYear || fy > MaxYear {
		return fail(&InputError{Kind: "from", Field: -1, Index: -1, Token: req.From, Message: fmt.Sprintf("Pick a start year from %d to %d.", MinYear, MaxYear)})
	}
	res.From = fmt.Sprintf("%04d-%02d", fy, int(fm))

	// Views and the zones whose clocks cron reads.
	runs := map[string]*runState{}
	views := make([]*viewState, len(names))
	for i := range names {
		ri := i
		if res.Mode == "shared" {
			ri = 0
		}
		r := runs[names[ri]]
		if r == nil {
			spec := *sched.Spec
			spec.Location = locs[ri]
			r = &runState{name: names[ri], loc: locs[ri], spec: spec, a: 1<<62 - 1, b: -(1 << 62)}
			runs[names[ri]] = r
		}
		v := &viewState{zone: names[i], loc: locs[i], run: r}
		v.ws = startOfDay(locs[i], fy, fm, 1)
		v.we = startOfDay(locs[i], fy+1, fm, 1)
		v.civil0 = dayNumber(fy, fm, 1)
		v.days = int(dayNumber(fy+1, fm, 1) - v.civil0)
		r.a, r.b = min(r.a, v.ws), max(r.b, v.we)
		views[i] = v
	}

	for _, name := range names {
		r := runs[name]
		if r == nil || r.firings != nil {
			continue
		}
		r.prepare(sched.Spec, now)
		res.Walked += len(r.firings)
	}

	for _, v := range views {
		res.Views = append(res.Views, v.build(now))
	}
	res.Never = true
	for _, r := range runs {
		if len(r.firings) > 0 || len(r.next) > 0 {
			res.Never = false
		}
	}
	return res, runs
}

// walk lists every firing in [a, b) using the library's Next.
func walk(spec *cron.SpecSchedule, a, b int64, stride time.Duration) []int64 {
	out := []int64{}
	t := spec.Next(time.Unix(a-1, 0))
	for !t.IsZero() && t.Unix() < b && len(out) < maxWalk {
		out = append(out, t.Unix())
		t = spec.Next(t.Add(stride))
	}
	return out
}

// guard is how far either side of a clock change the walk uses the real zone.
const guard = 2 * 86400

// walkSegmented returns exactly what walk returns with the real zone, faster.
// Go's embedded tz data is "slim": for future dates every offset lookup
// re-derives the zone’s rule, and robfig/cron’s Next does many lookups per
// call. Between clock changes the offset is constant, so there the library is
// handed a fixed-offset zone with that same offset (identical wall clock);
// within two days of each change it gets the real zone. A test checks the two
// walks match firing for firing.
func walkSegmented(spec cron.SpecSchedule, loc *time.Location, a, b int64, changes []change) []int64 {
	type seg struct {
		s, e int64
		real bool
	}
	var segs []seg
	cur := a
	for _, c := range changes {
		lo, hi := max(c.at-guard, a), min(c.at+guard, b)
		if lo > cur {
			segs = append(segs, seg{cur, lo, false})
		}
		if len(segs) > 0 && segs[len(segs)-1].real && segs[len(segs)-1].e >= lo {
			segs[len(segs)-1].e = max(segs[len(segs)-1].e, hi)
		} else {
			segs = append(segs, seg{max(lo, cur), hi, true})
		}
		cur = max(cur, hi)
	}
	if cur < b {
		segs = append(segs, seg{cur, b, false})
	}
	out := []int64{}
	for _, sg := range segs {
		if sg.s >= sg.e {
			continue
		}
		sp := spec
		if sg.real {
			sp.Location = loc
		} else {
			p := periodAt(loc, sg.s)
			sp.Location = time.FixedZone(p.abbr, p.off)
		}
		out = append(out, walk(&sp, sg.s, sg.e, walkStride)...)
		if len(out) >= maxWalk {
			return out[:maxWalk]
		}
	}
	return out
}

// dateMatches mirrors robfig/cron’s month and dayMatches rules for a civil date.
func dateMatches(s *cron.SpecSchedule, day int64) bool {
	y, m, d := civilDate(day)
	if s.Month&(1<<uint(m)) == 0 {
		return false
	}
	wd := time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Weekday()
	domMatch := s.Dom&(1<<uint(d)) != 0
	dowMatch := s.Dow&(1<<uint(wd)) != 0
	if s.Dom&starBit != 0 || s.Dow&starBit != 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

// anomaly records where the library's firings around one clock change differ
// from a plain wall-clock reading of the expression.
type anomaly struct {
	c        *change // nil if no clock change explains it
	day      int64
	skipped  []int     // wall minutes (run zone) that did not fire
	repeated []int     // wall minutes that fired more than once
	repInst  [][]int64 // the instants of each repeated minute
	extra    []int     // wall minutes that fired but the expression doesn’t name
	extraAt  []int64
}

func (a *anomaly) kind() string {
	switch {
	case len(a.extra) > 0:
		return "moved"
	case len(a.skipped) > 0:
		return "skip"
	case len(a.repeated) > 0:
		return "double"
	}
	return "quiet"
}

func (r *runState) prepare(spec *cron.SpecSchedule, now int64) {
	// Extend the walk to whole days on the run zone’s own clock, so each of
	// its dates can be compared with the expression in full.
	ca, cb := cursor{loc: r.loc}, cursor{loc: r.loc}
	dayA := civilDay(r.a + int64(ca.at(r.a).off))
	dayB := civilDay(r.b - 1 + int64(cb.at(r.b-1).off))
	y, m, d := civilDate(dayA)
	r.a = startOfDay(r.loc, y, m, d)
	y, m, d = civilDate(dayB + 1)
	r.b = startOfDay(r.loc, y, m, d)

	r.changes = clockChanges(r.loc, r.a, r.b)
	r.firings = walkSegmented(r.spec, r.loc, r.a, r.b, r.changes)
	r.classify(dayA, dayB)

	// The next firings from now, straight from the library.
	t := time.Unix(now, 0)
	for len(r.next) < NextCount {
		t = r.spec.Next(t)
		if t.IsZero() {
			break
		}
		r.next = append(r.next, t.Unix())
		r.nextNote = append(r.nextNote, repeatNote(r.loc, t.Unix()))
	}
}

// repeatNote says whether a firing's wall time is one that occurs twice
// because clocks went back.
func repeatNote(loc *time.Location, u int64) string {
	p := periodAt(loc, u)
	if p.start != -1<<63 {
		prev := periodAt(loc, p.start-1)
		if prev.off > p.off && u-p.start < int64(prev.off-p.off) {
			return "second"
		}
	}
	if p.end != 1<<63-1 {
		nxt := periodAt(loc, p.end)
		if nxt.off < p.off && p.end-u <= int64(p.off-nxt.off) {
			return "first"
		}
	}
	return ""
}

func (r *runState) classify(dayA, dayB int64) {
	n := int(dayB - dayA + 1)
	counts := make([]int, n)
	dayOf := make([]int32, len(r.firings))
	wall := make([]int16, len(r.firings))
	cur := cursor{loc: r.loc}
	for i, u := range r.firings {
		l := u + int64(cur.at(u).off)
		d := civilDay(l) - dayA
		dayOf[i], wall[i] = int32(d), int16(floorMod(l, 86400)/60)
		if d >= 0 && d < int64(n) {
			counts[d]++
		}
	}

	touched := map[int64]int{}
	for ci, c := range r.changes {
		lo, hi := c.wallSpan()
		for d := civilDay(lo); d <= civilDay(hi-1); d++ {
			touched[d-dayA] = ci
		}
	}
	perDay := count(r.spec.Hour) * count(r.spec.Minute)
	suspect := map[int64]bool{}
	for d := int64(0); d < int64(n); d++ {
		want := 0
		if dateMatches(&r.spec, dayA+d) {
			want = perDay
		}
		if _, ok := touched[d]; ok || counts[d] != want {
			suspect[d] = true
		}
	}
	if len(suspect) == 0 {
		return
	}
	byDay := map[int64][]int{}
	for i := range r.firings {
		if suspect[int64(dayOf[i])] {
			byDay[int64(dayOf[i])] = append(byDay[int64(dayOf[i])], i)
		}
	}
	days := make([]int64, 0, len(suspect))
	for d := range suspect {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })

	hours := members(r.spec.Hour, 0, 23)
	mins := members(r.spec.Minute, 0, 59)
	byChange := map[int]*anomaly{}
	for _, d := range days {
		var nominal [1440]bool
		if dateMatches(&r.spec, dayA+d) {
			for _, h := range hours {
				for _, m := range mins {
					nominal[h*60+m] = true
				}
			}
		}
		fired := map[int][]int64{}
		for _, i := range byDay[d] {
			fired[int(wall[i])] = append(fired[int(wall[i])], r.firings[i])
		}
		var skipped, repeated, extra []int
		for mnt := 0; mnt < 1440; mnt++ {
			f := fired[mnt]
			switch {
			case nominal[mnt] && len(f) == 0:
				skipped = append(skipped, mnt)
			case nominal[mnt] && len(f) > 1:
				repeated = append(repeated, mnt)
			case !nominal[mnt] && len(f) > 0:
				extra = append(extra, mnt)
			}
		}
		if len(skipped)+len(repeated)+len(extra) == 0 {
			continue
		}
		ci, ok := touched[d]
		if !ok {
			ci = -1 - int(d) // unexplained: its own entry per day
		}
		a := byChange[ci]
		if a == nil {
			a = &anomaly{day: dayA + d}
			if ok {
				a.c = &r.changes[ci]
			}
			byChange[ci] = a
			r.anoms = append(r.anoms, a)
		}
		a.skipped = append(a.skipped, skipped...)
		for _, mnt := range repeated {
			a.repeated = append(a.repeated, mnt)
			a.repInst = append(a.repInst, fired[mnt])
		}
		for _, mnt := range extra {
			a.extra = append(a.extra, mnt)
			a.extraAt = append(a.extraAt, fired[mnt]...)
		}
	}
}

func (r *runState) anomalyAt(at int64) *anomaly {
	for _, a := range r.anoms {
		if a.c != nil && a.c.at == at {
			return a
		}
	}
	return nil
}

func (v *viewState) build(now int64) View {
	r := v.run
	// Lists are never null in the JSON, so the page can iterate them directly.
	out := View{Zone: v.zone, RunZone: r.name, Start: fmtISO(v.civil0), Days: v.days, Today: -1, Dial: [][2]int{}, Next: []Firing{}}
	pn := periodAt(v.loc, now)
	out.Abbr, out.Offset = pn.abbr, fmtOffset(pn.off)
	ln := now + int64(pn.off)
	if d := civilDay(ln) - v.civil0; d >= 0 && d < int64(v.days) {
		out.Today = int(d)
	}
	out.NowMinute = int(floorMod(ln, 86400) / 60)

	bins := make([][BinsPerDay / 4]byte, v.days)
	times := make([][maxTimes]int16, v.days)
	out.Counts = make([]int, v.days)
	var dial [1440]int
	lo := sort.Search(len(r.firings), func(i int) bool { return r.firings[i] >= v.ws })
	hi := sort.Search(len(r.firings), func(i int) bool { return r.firings[i] >= v.we })
	cur := cursor{loc: v.loc}
	for _, u := range r.firings[lo:hi] {
		l := u + int64(cur.at(u).off)
		d := civilDay(l) - v.civil0
		if d < 0 || d >= int64(v.days) {
			continue
		}
		mnt := int(floorMod(l, 86400) / 60)
		b := mnt / BinMinutes
		bins[d][b/4] |= 1 << uint(b%4)
		if out.Counts[d] < maxTimes {
			times[d][out.Counts[d]] = int16(mnt)
		}
		out.Counts[d]++
		dial[mnt]++
		out.Total++
	}
	out.Times = make([]string, v.days)
	for d, n := range out.Counts {
		if n == 0 || n > maxTimes {
			continue
		}
		ts := make([]string, n)
		for i := range ts {
			ts[i] = fmtMinute(int(times[d][i]))
		}
		out.Times[d] = strings.Join(ts, " ")
	}
	out.Bins = make([]string, v.days)
	const hexd = "0123456789abcdef"
	for d := range bins {
		if out.Counts[d] == 0 {
			continue
		}
		var sb [BinsPerDay / 4]byte
		for k, nib := range bins[d] {
			sb[k] = hexd[nib]
		}
		out.Bins[d] = string(sb[:])
	}
	for mnt, c := range dial {
		if c > 0 {
			out.Dial = append(out.Dial, [2]int{mnt, c})
		}
	}

	// Months.
	yy, mm, _ := civilDate(v.civil0)
	for i := 0; i < 12; i++ {
		t := time.Date(yy, mm+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		y, m := t.Year(), t.Month()
		fd := int(dayNumber(y, m, 1) - v.civil0)
		nd := int(dayNumber(y, m+1, 1) - dayNumber(y, m, 1))
		c := 0
		for d := fd; d < fd+nd && d < v.days; d++ {
			c += out.Counts[d]
		}
		out.Months = append(out.Months, Month{Year: y, Month: int(m), Label: shortMonths[m], First: fd, Days: nd, Count: c})
	}

	out.Seams = v.seams(lo, hi)
	if out.Seams == nil {
		out.Seams = []Seam{}
	}

	for i, u := range r.next {
		p := periodAt(v.loc, u)
		l := u + int64(p.off)
		f := Firing{At: u * 1000, Date: fmtDate(civilDay(l)), Time: fmtMinute(int(floorMod(l, 86400) / 60)), Abbr: p.abbr, Offset: fmtOffset(p.off)}
		rp := periodAt(r.loc, u)
		rl := u + int64(rp.off)
		rw := fmtMinute(int(floorMod(rl, 86400) / 60))
		switch r.nextNote[i] {
		case "first":
			f.Note = fmt.Sprintf("first of two %s runs in %s: clocks go back after it", rw, r.name)
		case "second":
			f.Note = fmt.Sprintf("%s again in %s, after clocks go back", rw, r.name)
		}
		out.Next = append(out.Next, f)
	}
	if len(r.next) == 0 {
		out.NextNote = "robfig/cron finds no firing in the next five years (its search limit), so this schedule never runs."
	}
	return out
}

// seams builds the clock-change seams, with the firings each one affected.
func (v *viewState) seams(lo, hi int) []Seam {
	r := v.run
	same := v.zone == r.name
	var seams []Seam
	var marks []Mark
	addMark := func(day, mnt int, kind string) {
		for _, m := range marks {
			if m.Day == day && m.Minute == mnt && m.Kind == kind {
				return
			}
		}
		if len(marks) < maxMarks {
			marks = append(marks, Mark{Day: day, Minute: mnt, Kind: kind})
		}
	}
	vc := cursor{loc: v.loc}
	localOf := func(u int64) (day int, mnt int) {
		// the reading on this view's clock just before u
		p := vc.at(u - 1)
		l := u + int64(p.off)
		return int(civilDay(l) - v.civil0), int(floorMod(l, 86400) / 60)
	}
	inWindow := func(u int64) bool { return u >= v.ws && u < v.we }

	// Every instant where either clock changed.
	type moment struct {
		at      int64
		run, vw *change
	}
	ms := map[int64]*moment{}
	for i := range r.changes {
		c := &r.changes[i]
		if inWindow(c.at) {
			ms[c.at] = &moment{at: c.at, run: c}
		}
	}
	if !same {
		for _, c := range clockChanges(v.loc, v.ws, v.we) {
			c := c
			if m := ms[c.at]; m != nil {
				m.vw = &c
			} else {
				ms[c.at] = &moment{at: c.at, vw: &c}
			}
		}
	}
	keys := make([]int64, 0, len(ms))
	for k := range ms {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	firingsBefore := func(at int64) bool { return lo < hi && r.firings[lo] < at }
	firingsAfter := func(at int64) bool { return lo < hi && r.firings[hi-1] >= at }

	for _, at := range keys {
		m := ms[at]
		marks = nil
		day, mnt := localOf(at)
		s := Seam{Day: day, Minute: mnt, At: at * 1000, Time: fmtMinute(mnt)}
		if day >= 0 && day < v.days {
			s.Date = fmtDate(v.civil0 + int64(day))
		}
		var parts []string
		var zones []string

		if m.run != nil {
			zones = append(zones, r.name)
			a := r.anomalyAt(at)
			kind := "quiet"
			if a != nil {
				kind = a.kind()
			}
			s.Kind = kind
			s.Red = kind != "quiet"
			parts = append(parts, runChangeText(r.name, *m.run, a, same, v.zone))
			if a != nil {
				s.Label = anomalyLabel(a)
				switch kind {
				case "skip", "moved":
					if same {
						for i, w := range a.skipped {
							if i < maxWallList {
								addMark(day, w, "skip")
							}
						}
					} else {
						addMark(day, mnt, "skip")
					}
					for _, u := range a.extraAt {
						ed, em := localOf(u + 1)
						addMark(ed, em, "moved")
					}
				case "double":
					for i, inst := range a.repInst {
						if i >= maxWallList {
							break
						}
						for _, u := range inst {
							dd, dm := localOf(u + 1)
							addMark(dd, dm, "double")
						}
					}
				}
			}
		}
		if m.vw != nil {
			zones = append(zones, v.zone)
		}
		if !same {
			// How the reading of the run clock on this clock moves.
			rb, ra := periodAt(r.loc, at-1).off, periodAt(r.loc, at).off
			vb, va := periodAt(v.loc, at-1).off, periodAt(v.loc, at).off
			shift := (va - ra) - (vb - rb)
			if m.vw != nil {
				c := *m.vw
				parts = append(parts, fmt.Sprintf("Clocks here %s %s at %s, to %s (%s becomes %s).", goWord(c.delta()), fmtDuration(c.delta()), wallAt(c.at, c.before.off), wallAt(c.at, c.after.off), c.before.abbr, c.after.abbr))
			}
			switch {
			case shift == 0:
				parts = append(parts, "Both clocks move together, so readings here don’t change.")
			case firingsBefore(at) && firingsAfter(at):
				dir := "later"
				if shift < 0 {
					dir = "earlier"
				}
				parts = append(parts, fmt.Sprintf("From here on, its firings read %s %s on this clock.", fmtDuration(shift), dir))
				if s.Kind == "" || s.Kind == "quiet" {
					s.Kind, s.Red = "shift", true
					s.Label = fmt.Sprintf("reads %s %s", shortDuration(shift), dir)
				}
			default:
				parts = append(parts, "This window has no firings on both sides of it, so nothing visibly moves.")
			}
		}
		if s.Kind == "" {
			s.Kind = "quiet"
		}
		if s.Label == "" {
			d := 0
			if m.vw != nil {
				d = m.vw.delta()
			} else if m.run != nil {
				d = m.run.delta()
			}
			s.Label = "clocks " + signedShort(d)
		}
		s.Zone = strings.Join(zones, " and ")
		s.Text = strings.Join(parts, " ")
		s.Marks = marks
		if s.Marks == nil {
			s.Marks = []Mark{}
		}
		seams = append(seams, s)
	}

	// Differences with no clock change behind them (not expected from the
	// library, but reported if they ever occur).
	for _, a := range r.anoms {
		if a.c != nil {
			continue
		}
		at := startOfDayNum(r.loc, a.day)
		if !inWindow(at) {
			continue
		}
		day, _ := localOf(at + 1)
		seams = append(seams, Seam{Kind: a.kind(), Red: true, Day: day, At: at * 1000, Date: fmtDate(a.day), Zone: r.name, Label: anomalyLabel(a),
			Text: fmt.Sprintf("On %s, robfig/cron’s firings in %s differ from a plain reading of the expression (%s).", fmtDate(a.day), r.name, anomalyDetail(a))})
	}
	sort.SliceStable(seams, func(i, j int) bool { return seams[i].At < seams[j].At })
	return seams
}

func startOfDayNum(loc *time.Location, day int64) int64 {
	y, m, d := civilDate(day)
	return startOfDay(loc, y, m, d)
}

// wallAt is the clock reading of instant u under offset off.
func wallAt(u int64, off int) string { return fmtMinute(int(floorMod(u+int64(off), 86400) / 60)) }

func goWord(delta int) string {
	if delta > 0 {
		return "go forward"
	}
	return "go back"
}

func shortDuration(sec int) string {
	if sec < 0 {
		sec = -sec
	}
	if sec%3600 == 0 {
		return fmt.Sprintf("%d h", sec/3600)
	}
	if sec < 3600 {
		return fmt.Sprintf("%d min", sec/60)
	}
	return fmt.Sprintf("%d h %02d", sec/3600, sec%3600/60)
}

func signedShort(sec int) string {
	if sec >= 0 {
		return "+" + shortDuration(sec)
	}
	return "−" + shortDuration(sec)
}

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

func anomalyLabel(a *anomaly) string {
	switch a.kind() {
	case "moved":
		return "moved"
	case "skip":
		if len(a.skipped) == 1 {
			return fmtMinute(a.skipped[0]) + " skipped"
		}
		return fmt.Sprintf("%d skipped", len(a.skipped))
	case "double":
		if len(a.repeated) == 1 {
			return fmtMinute(a.repeated[0]) + " twice"
		}
		return fmt.Sprintf("%d ran twice", len(a.repeated))
	}
	return ""
}

func anomalyDetail(a *anomaly) string {
	var d []string
	if len(a.skipped) > 0 {
		d = append(d, "skips "+wallList(a.skipped))
	}
	if len(a.repeated) > 0 {
		d = append(d, "fires twice at "+wallList(a.repeated))
	}
	if len(a.extra) > 0 {
		d = append(d, "fires at "+wallList(a.extra)+", which the expression doesn’t name")
	}
	return strings.Join(d, "; ")
}

// runChangeText explains a clock change on the zone cron runs in.
func runChangeText(runName string, c change, a *anomaly, same bool, viewZone string) string {
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
		who = fmt.Sprintf("In %s, where this schedule runs, clocks", runName)
	}
	head := fmt.Sprintf("%s %s %s at %s, to %s (%s becomes %s; %s).", who, goWord(c.delta()), fmtDuration(c.delta()), from, to, c.before.abbr, c.after.abbr, why)

	forward := c.delta() > 0
	if a == nil {
		if forward {
			return head + fmt.Sprintf(" No firing falls in the missing %s.", span)
		}
		return head + fmt.Sprintf(" No firing falls in the repeated %s.", span)
	}
	switch a.kind() {
	case "skip":
		if len(a.skipped) == 1 {
			return head + fmt.Sprintf(" %s doesn’t happen, so robfig/cron skips the %s firing.", span, fmtMinute(a.skipped[0]))
		}
		return head + fmt.Sprintf(" %s doesn’t happen, so robfig/cron skips %d firings (%s).", span, len(a.skipped), wallList(a.skipped))
	case "double":
		gap := fmtDuration(c.delta())
		if len(a.repeated) == 1 {
			return head + fmt.Sprintf(" %s happens twice, and robfig/cron fires at %s both times, %s apart.", span, fmtMinute(a.repeated[0]), gap)
		}
		return head + fmt.Sprintf(" %s happens twice, and robfig/cron fires both times at %s.", span, wallList(a.repeated))
	}
	return head + " Here robfig/cron " + anomalyDetail(a) + "."
}
