package almanac

import "strconv"

// A small hand-written JSON encoder for Result. encoding/json would add about
// a megabyte to the WebAssembly binary; json_test.go checks this output
// against encoding/json for the same values.

type jw struct{ b []byte }

func (w *jw) raw(s string) { w.b = append(w.b, s...) }

func (w *jw) str(s string) {
	const hexd = "0123456789abcdef"
	w.b = append(w.b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			w.b = append(w.b, '\\', c)
		case c < 0x20:
			w.b = append(w.b, '\\', 'u', '0', '0', hexd[c>>4], hexd[c&15])
		case c == '<' || c == '>' || c == '&':
			w.b = append(w.b, '\\', 'u', '0', '0', hexd[c>>4], hexd[c&15])
		default:
			w.b = append(w.b, c)
		}
	}
	w.b = append(w.b, '"')
}

func (w *jw) int(n int64) { w.b = strconv.AppendInt(w.b, n, 10) }

func (w *jw) bool(v bool) { w.b = strconv.AppendBool(w.b, v) }

// obj writes {"k":v,...}; each field func writes its own value.
type field struct {
	k    string
	v    func()
	omit bool
}

func (w *jw) obj(fs ...field) {
	w.b = append(w.b, '{')
	first := true
	for _, f := range fs {
		if f.omit {
			continue
		}
		if !first {
			w.b = append(w.b, ',')
		}
		first = false
		w.str(f.k)
		w.b = append(w.b, ':')
		f.v()
	}
	w.b = append(w.b, '}')
}

func arr[T any](w *jw, xs []T, each func(T)) {
	if xs == nil {
		w.raw("null")
		return
	}
	w.b = append(w.b, '[')
	for i, x := range xs {
		if i > 0 {
			w.b = append(w.b, ',')
		}
		each(x)
	}
	w.b = append(w.b, ']')
}

// JSON encodes the result.
func (r *Result) JSON() []byte {
	w := &jw{b: make([]byte, 0, 1<<14)}
	w.obj(
		field{"error", func() { r.Error.write(w) }, r.Error == nil},
		field{"expr", func() { w.str(r.Expr) }, false},
		field{"fields", func() { arr(w, r.Fields[:], w.str) }, false},
		field{"description", func() { w.str(r.Description) }, false},
		field{"mode", func() { w.str(r.Mode) }, false},
		field{"from", func() { w.str(r.From) }, false},
		field{"perDay", func() { w.int(int64(r.PerDay)) }, false},
		field{"never", func() { w.bool(r.Never) }, false},
		field{"walked", func() { w.int(int64(r.Walked)) }, false},
		field{"truncated", func() { w.bool(r.Truncated) }, false},
		field{"views", func() { arr(w, r.Views, func(v View) { v.write(w) }) }, false},
	)
	return w.b
}

func (e *InputError) write(w *jw) {
	w.obj(
		field{"kind", func() { w.str(e.Kind) }, false},
		field{"field", func() { w.int(int64(e.Field)) }, false},
		field{"index", func() { w.int(int64(e.Index)) }, false},
		field{"token", func() { w.str(e.Token) }, e.Token == ""},
		field{"message", func() { w.str(e.Message) }, false},
	)
}

func (v View) write(w *jw) {
	ints := func(n int) { w.int(int64(n)) }
	w.obj(
		field{"zone", func() { w.str(v.Zone) }, false},
		field{"runZone", func() { w.str(v.RunZone) }, false},
		field{"start", func() { w.str(v.Start) }, false},
		field{"days", func() { ints(v.Days) }, false},
		field{"today", func() { ints(v.Today) }, false},
		field{"nowMinute", func() { ints(v.NowMinute) }, false},
		field{"abbr", func() { w.str(v.Abbr) }, false},
		field{"offset", func() { w.str(v.Offset) }, false},
		field{"months", func() {
			arr(w, v.Months, func(m Month) {
				w.obj(
					field{"year", func() { ints(m.Year) }, false},
					field{"month", func() { ints(m.Month) }, false},
					field{"label", func() { w.str(m.Label) }, false},
					field{"first", func() { ints(m.First) }, false},
					field{"days", func() { ints(m.Days) }, false},
					field{"count", func() { ints(m.Count) }, false},
				)
			})
		}, false},
		field{"bins", func() { arr(w, v.Bins, w.str) }, false},
		field{"counts", func() { arr(w, v.Counts, ints) }, false},
		field{"times", func() { arr(w, v.Times, w.str) }, false},
		field{"dial", func() { arr(w, v.Dial, func(p [2]int) { arr(w, p[:], ints) }) }, false},
		field{"seams", func() {
			arr(w, v.Seams, func(s Seam) {
				w.obj(
					field{"kind", func() { w.str(s.Kind) }, false},
					field{"red", func() { w.bool(s.Red) }, false},
					field{"day", func() { ints(s.Day) }, false},
					field{"minute", func() { ints(s.Minute) }, false},
					field{"at", func() { w.int(s.At) }, false},
					field{"date", func() { w.str(s.Date) }, false},
					field{"time", func() { w.str(s.Time) }, false},
					field{"zone", func() { w.str(s.Zone) }, false},
					field{"label", func() { w.str(s.Label) }, false},
					field{"text", func() { w.str(s.Text) }, false},
					field{"marks", func() {
						arr(w, s.Marks, func(m Mark) {
							w.obj(
								field{"day", func() { ints(m.Day) }, false},
								field{"minute", func() { ints(m.Minute) }, false},
								field{"kind", func() { w.str(m.Kind) }, false},
							)
						})
					}, false},
				)
			})
		}, false},
		field{"total", func() { ints(v.Total) }, false},
		field{"next", func() {
			arr(w, v.Next, func(f Firing) {
				w.obj(
					field{"at", func() { w.int(f.At) }, false},
					field{"date", func() { w.str(f.Date) }, false},
					field{"time", func() { w.str(f.Time) }, false},
					field{"abbr", func() { w.str(f.Abbr) }, false},
					field{"offset", func() { w.str(f.Offset) }, false},
					field{"note", func() { w.str(f.Note) }, f.Note == ""},
				)
			})
		}, false},
		field{"nextNote", func() { w.str(v.NextNote) }, v.NextNote == ""},
	)
}
