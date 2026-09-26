// Package almanac turns a standard five-field cron expression into a year of
// firings across time zones. The schedule semantics are those of
// github.com/robfig/cron/v3 (the parser Kubernetes CronJob uses): this package
// parses with cron.ParseStandard and walks firings with SpecSchedule.Next, then
// measures where daylight-saving changes made the library skip, repeat or
// move a wall-clock time.
package almanac

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// MaxExprLen caps the expression length. Real crontab lines are far shorter.
const MaxExprLen = 120

// FieldNames names the five standard fields, in order.
var FieldNames = [5]string{"minute", "hour", "day of month", "month", "day of week"}

// ParseError is a parse failure with the offending field when it is known.
type ParseError struct {
	Field   int    `json:"field"` // 0–4, or -1 for the whole expression
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
}

func (e *ParseError) Error() string { return e.Message }

// Schedule is a parsed expression.
type Schedule struct {
	Spec       *cron.SpecSchedule
	Fields     [5]string // the five tokens as written (descriptors are expanded)
	Descriptor string    // "@daily" etc., or ""
}

// Descriptors robfig/cron accepts, expanded to their five-field meaning.
var descriptors = map[string][5]string{
	"@yearly":   {"0", "0", "1", "1", "*"},
	"@annually": {"0", "0", "1", "1", "*"},
	"@monthly":  {"0", "0", "1", "*", "*"},
	"@weekly":   {"0", "0", "*", "*", "0"},
	"@daily":    {"0", "0", "*", "*", "*"},
	"@midnight": {"0", "0", "*", "*", "*"},
	"@hourly":   {"0", "*", "*", "*", "*"},
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 || c > 0x7e) && c != '\t' {
			return false
		}
	}
	return true
}

// Parse validates expr and parses it with robfig/cron’s standard parser.
// Errors name the field at fault in plain English.
func Parse(expr string) (s *Schedule, err error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, &ParseError{Field: -1, Message: "Type a cron expression, for example 0 9 * * 1-5."}
	}
	if len(expr) > MaxExprLen {
		return nil, &ParseError{Field: -1, Message: fmt.Sprintf("That expression is %d characters long; the limit here is %d.", len(expr), MaxExprLen)}
	}
	if !printableASCII(expr) {
		return nil, &ParseError{Field: -1, Message: "Cron expressions use plain ASCII: digits, letters, and * , - / ?"}
	}
	up := strings.ToUpper(expr)
	if strings.HasPrefix(up, "TZ=") || strings.HasPrefix(up, "CRON_TZ=") {
		return nil, &ParseError{Field: -1, Message: "Leave out the TZ= prefix and choose the zone in the zone fields instead."}
	}

	var fields [5]string
	descriptor := ""
	if strings.HasPrefix(expr, "@") {
		lower := strings.ToLower(expr)
		if strings.HasPrefix(lower, "@every") {
			return nil, &ParseError{Field: -1, Message: "@every runs at a fixed interval counted from when the scheduler started, not on the calendar, so it has no almanac. Use a five-field expression instead."}
		}
		f, ok := descriptors[lower]
		if !ok {
			return nil, &ParseError{Field: -1, Token: expr, Message: fmt.Sprintf("Unknown shortcut %q. This parser knows @yearly, @monthly, @weekly, @daily and @hourly.", expr)}
		}
		fields, descriptor = f, lower
	} else {
		parts := strings.Fields(expr)
		switch {
		case len(parts) == 6:
			return nil, &ParseError{Field: -1, Message: "That has 6 fields. Standard cron has 5 (minute hour day month weekday); a leading seconds field, as in Quartz, isn’t supported here."}
		case len(parts) == 7:
			return nil, &ParseError{Field: -1, Message: "That has 7 fields, like a Quartz expression with seconds and year. Standard cron has 5: minute hour day month weekday."}
		case len(parts) != 5:
			return nil, &ParseError{Field: -1, Message: fmt.Sprintf("Expected 5 fields (minute hour day month weekday), found %d.", len(parts))}
		}
		copy(fields[:], parts)
	}

	spec, perr := parseStandard(strings.Join(fields[:], " "))
	if perr != nil {
		return nil, locate(fields, perr)
	}
	return &Schedule{Spec: spec, Fields: fields, Descriptor: descriptor}, nil
}

// parseStandard calls the library and turns a panic into an error. (Kubernetes
// wraps the same call in a recover; some malformed inputs make it panic.)
func parseStandard(spec string) (s *cron.SpecSchedule, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("the parser could not read this expression")
		}
	}()
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, err
	}
	spec2, ok := sched.(*cron.SpecSchedule)
	if !ok {
		return nil, fmt.Errorf("not a calendar schedule")
	}
	return spec2, nil
}

// locate finds which field the library objected to by parsing each field on
// its own (the other four set to *), then words the library's message plainly.
func locate(fields [5]string, whole error) *ParseError {
	for i := range fields {
		probe := [5]string{"*", "*", "*", "*", "*"}
		probe[i] = fields[i]
		if _, err := parseStandard(strings.Join(probe[:], " ")); err != nil {
			return &ParseError{Field: i, Token: fields[i], Message: friendly(i, fields[i], err.Error())}
		}
	}
	return &ParseError{Field: -1, Message: "The parser rejected this: " + whole.Error()}
}

func friendly(field int, token, msg string) string {
	name := FieldNames[field]
	head := fmt.Sprintf("%s%s “%s”: ", strings.ToUpper(name[:1]), name[1:], token)
	var a, b int
	switch {
	case scan(msg, "end of range (%d) above maximum (%d)", &a, &b):
		s := fmt.Sprintf("%d is above the largest %s, %d.", a, name, b)
		switch field {
		case 0:
			s += " Minutes run 0–59."
		case 1:
			s += " Hours run 0–23."
		case 4:
			s += " This parser counts Sunday as 0 and Saturday as 6; 7 isn’t accepted, so use 0 or SUN."
		}
		return head + s
	case scan(msg, "beginning of range (%d) below minimum (%d)", &a, &b):
		return head + fmt.Sprintf("%d is below the smallest %s, %d.", a, name, b)
	case scan(msg, "beginning of range (%d) beyond end of range (%d)", &a, &b):
		return head + fmt.Sprintf("the range runs backwards (%d comes after %d). Cron ranges can’t wrap around; split it, for example 22-23,0-2.", a, b)
	case strings.HasPrefix(msg, "failed to parse int from "):
		bad := strings.TrimPrefix(msg, "failed to parse int from ")
		if i := strings.Index(bad, ": "); i >= 0 {
			bad = bad[:i]
		}
		if bad == "" {
			return head + "there is an empty value. Check for a dangling - or /."
		}
		s := fmt.Sprintf("“%s” isn’t a number", bad)
		switch field {
		case 3:
			s += " or a month name like JAN"
		case 4:
			s += " or a day name like MON"
		}
		return head + s + "."
	case strings.HasPrefix(msg, "too many hyphens"):
		return head + "too many hyphens. A range is written low-high, like 1-5."
	case strings.HasPrefix(msg, "too many slashes"):
		return head + "too many slashes. A step is written range/step, like */15."
	case strings.HasPrefix(msg, "step of range should be a positive number"):
		return head + "the step after / must be 1 or more."
	}
	return head + msg + "."
}

// scan matches a library message against a pattern with two %d verbs.
func scan(msg, pattern string, a, b *int) bool {
	n, err := fmt.Sscanf(msg, pattern, a, b)
	return err == nil && n == 2
}
