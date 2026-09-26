package almanac

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/robfig/cron/v3"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		expr   string
		fields string
	}{
		{"0 9 * * 1-5", "0 9 * * 1-5"},
		{"  30   2 * *\t*  ", "30 2 * * *"},
		{"*/15 * * * *", "*/15 * * * *"},
		{"0 9 * * MON-FRI", "0 9 * * MON-FRI"},
		{"0 0 1 jan,Jul ?", "0 0 1 jan,Jul ?"},
		{"5/15 8-18/2 1,15 */3 sun", "5/15 8-18/2 1,15 */3 sun"},
		{"@daily", "0 0 * * *"},
		{"@weekly", "0 0 * * 0"},
		{"@Yearly", "0 0 1 1 *"},
		{"@hourly", "0 * * * *"},
	}
	for _, c := range cases {
		s, err := Parse(c.expr)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", c.expr, err)
			continue
		}
		if got := strings.Join(s.Fields[:], " "); got != c.fields {
			t.Errorf("Parse(%q) fields = %q, want %q", c.expr, got, c.fields)
		}
	}
}

// The bit sets come from the library itself; spot-check a few.
func TestParseUsesLibraryBitSets(t *testing.T) {
	s, err := Parse("5/15 8-18/2 * * MON-FRI")
	if err != nil {
		t.Fatal(err)
	}
	if got := members(s.Spec.Minute, 0, 59); !equalInts(got, []int{5, 20, 35, 50}) {
		t.Errorf("minutes = %v", got)
	}
	if got := members(s.Spec.Hour, 0, 23); !equalInts(got, []int{8, 10, 12, 14, 16, 18}) {
		t.Errorf("hours = %v", got)
	}
	if got := members(s.Spec.Dow, 0, 6); !equalInts(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("weekdays = %v", got)
	}
	if s.Spec.Dom&starBit == 0 || s.Spec.Dow&starBit != 0 {
		t.Errorf("star bits: dom %v dow %v", s.Spec.Dom&starBit != 0, s.Spec.Dow&starBit != 0)
	}
	if s.Spec.Second != 1 {
		t.Errorf("standard cron fires at second 0 only, got %b", s.Spec.Second)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		expr  string
		field int
		want  string
	}{
		{"", -1, "Type a cron expression"},
		{"   ", -1, "Type a cron expression"},
		{"60 * * * *", 0, "Minute “60”: 60 is above the largest minute, 59"},
		{"0 24 * * *", 1, "Hours run 0–23"},
		{"0 0 32 * *", 2, "Day of month “32”: 32 is above the largest day of month, 31"},
		{"0 0 0 * *", 2, "0 is below the smallest day of month, 1"},
		{"0 0 * 13 *", 3, "Month “13”"},
		{"0 0 * 0 *", 3, "0 is below the smallest month, 1"},
		{"0 0 * * 7", 4, "7 isn’t accepted, so use 0 or SUN"},
		{"a b c d e", 0, "“a” isn’t a number"},
		{"0 0 * JANX *", 3, "isn’t a number or a month name like JAN"},
		{"0 0 * * FUNDAY", 4, "or a day name like MON"},
		{"5-1 * * * *", 0, "the range runs backwards (5 comes after 1)"},
		{"0 22-2 * * *", 1, "Cron ranges can’t wrap around"},
		{"*/0 * * * *", 0, "the step after / must be 1 or more"},
		{"1-2-3 * * * *", 0, "too many hyphens"},
		{"*/2/3 * * * *", 0, "too many slashes"},
		{"0 -5 * * *", 1, "empty value"},
		{"0 0 * *", -1, "Expected 5 fields (minute hour day month weekday), found 4"},
		{"0 0 0 * * *", -1, "a leading seconds field, as in Quartz, isn’t supported"},
		{"0 0 0 * * ? 2027", -1, "7 fields, like a Quartz expression"},
		{"TZ=UTC 0 0 * * *", -1, "Leave out the TZ= prefix"},
		{"CRON_TZ=Asia/Tokyo 0 6 * * *", -1, "Leave out the TZ= prefix"},
		{"TZ=", -1, "Leave out the TZ= prefix"}, // this input makes the library itself panic
		{"@every 1h30m", -1, "@every runs at a fixed interval"},
		{"@often", -1, "Unknown shortcut"},
		{"0 9 * * 1–5", -1, "plain ASCII"}, // an en dash pasted from a document
		{strings.Repeat("1,", 70) + "1 * * * *", -1, "the limit here is 120"},
	}
	for _, c := range cases {
		_, err := Parse(c.expr)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q): want a ParseError, got %v", c.expr, err)
			continue
		}
		if pe.Field != c.field {
			t.Errorf("Parse(%q): field %d, want %d (%s)", c.expr, pe.Field, c.field, pe.Message)
		}
		if !strings.Contains(pe.Message, c.want) {
			t.Errorf("Parse(%q): message %q does not contain %q", c.expr, pe.Message, c.want)
		}
	}
}

// Whatever is typed, Parse returns a schedule or a ParseError; it never panics.
func TestParseNeverPanics(t *testing.T) {
	alphabet := []string{"0", "1", "5", "7", "12", "23", "31", "59", "60", "*", "?", ",", "-", "/", " ", "  ", "MON", "jan", "@", "TZ=", "x", "\t", "L", "#"}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		var sb strings.Builder
		for n := rng.Intn(14); n >= 0; n-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		expr := sb.String()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q) panicked: %v", expr, r)
				}
			}()
			s, err := Parse(expr)
			if err == nil && s == nil {
				t.Fatalf("Parse(%q): nil schedule and nil error", expr)
			}
			var pe *ParseError
			if err != nil && !errors.As(err, &pe) {
				t.Fatalf("Parse(%q): error is not a ParseError: %v", expr, err)
			}
		}()
	}
}

// The library panics on a bare "TZ=" (it slices up to a space that isn't
// there). Parse refuses TZ= prefixes before reaching it, and parseStandard
// recovers anyway, as Kubernetes does around the same call.
func TestLibraryPanicIsRecovered(t *testing.T) {
	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		cron.ParseStandard("TZ=")
		return false
	}()
	if !panicked {
		t.Fatal("expected robfig/cron v3.0.1 to panic on a bare TZ=; if it no longer does, update the docs")
	}
	if _, err := parseStandard("TZ="); err == nil {
		t.Fatal("parseStandard should turn the panic into an error")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"0 9 * * 1-5", "TZ=", "*/0 * * * *", "@every 1h", "1-2-3 */4/5 ? * L"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		s, err := Parse(expr)
		if err == nil && s == nil {
			t.Fatal("nil schedule and nil error")
		}
	})
}

func equalInts(a, b []int) bool {
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
