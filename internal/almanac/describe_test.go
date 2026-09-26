package almanac

import "testing"

func TestDescribe(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"0 9 * * 1-5", "At 09:00, Monday through Friday"},
		{"0 9 * * MON-FRI", "At 09:00, Monday through Friday"},
		{"30 2 * * *", "At 02:30, every day"},
		{"* * * * *", "Every minute"},
		{"*/15 * * * *", "Every 15 minutes"},
		{"*/7 * * * *", "Every 7 minutes (the count restarts at the top of each hour)"},
		{"0 * * * *", "Every hour, on the hour"},
		{"@hourly", "Every hour, on the hour"},
		{"5 * * * *", "Every hour at :05"},
		{"5/15 * * * *", "Every hour at :05, :20, :35 and :50"},
		{"0 9,17 * * *", "At 09:00 and 17:00, every day"},
		{"15 */6 * * *", "At 00:15, 06:15, 12:15 and 18:15, every day"},
		{"0 */2 * * *", "Every 2 hours on the hour"},
		{"0 9-17 * * 1-5", "Every hour on the hour from 09:00 through 17:00, Monday through Friday"},
		{"*/10 9-17 * * *", "Every 10 minutes from 09:00 through 17:50"},
		{"0,30 9-17 * * 1-5", "At :00 and :30 past each hour from 09:00 through 17:30, Monday through Friday"},
		{"* 9 * * *", "Every minute from 09:00 through 09:59"},
		{"0 22 * * 0,6", "At 22:00, on Saturday and Sunday"},
		{"0 0 * * 1,3,5", "At 00:00, on Monday, Wednesday and Friday"},
		{"@weekly", "At 00:00, every Sunday"},
		{"0 0 1 * *", "At 00:00, on the 1st of every month"},
		{"@monthly", "At 00:00, on the 1st of every month"},
		{"0 9 1,15 * *", "At 09:00, on the 1st and 15th of every month"},
		{"0 0 1 1 *", "At 00:00, on 1 January"},
		{"@yearly", "At 00:00, on 1 January"},
		{"0 0 1 */3 *", "At 00:00, on the 1st of January, April, July and October"},
		{"0 8 * 1 *", "At 08:00, every day in January"},
		{"0 0 * 6-8 *", "At 00:00, every day from June through August"},
		{"0 12 29 2 *", "At 12:00, on 29 February (leap years only)"},
		{"0 0 31 * *", "At 00:00, on the 31st of every month (skipped in months without a 31st)"},
		{"0 0 30 2 *", "At 00:00, on 30 February, a date that never occurs, so this never fires"},
		// Both day fields restricted: cron (and robfig/cron) matches EITHER.
		{"0 0 1 * 1", "At 00:00, on the 1st of every month, and also every Monday (cron matches either day rule)"},
		{"0 9 1-31 * 1-5", "At 09:00, every day (with both day fields set, cron matches either, and one covers every day)"},
		// Either-rule with chosen months: both rules read within those months.
		{"0 0 1,15 6 1", "At 00:00, on the 1st and 15th of June, and also every Monday in June (cron matches either day rule)"},
		{"0 0 1,15 3-5 1", "At 00:00, on the 1st and 15th of March through May, and also every Monday from March through May (cron matches either day rule)"},
		{"0 0 29 2 1", "At 00:00, on 29 February, and also every Monday in February (cron matches either day rule)"},
		{"0 0 1 3-5 *", "At 00:00, on the 1st of March through May"},
		{"0 12 * 2 0", "At 12:00, every Sunday in February"},
		{"0 12 * JAN-MAR MON-FRI", "At 12:00, Monday through Friday from January through March"},
		// Runs of minutes read as ranges.
		{"0-10 * * * *", "Every hour, at minutes :00 through :10"},
		{"0-10,30 * * * *", "Every hour, at minutes :00 through :10 and :30"},
		{"0-10 9-17 * * *", "At minutes :00 through :10 past each hour from 09:00 through 17:10, every day"},
	}
	for _, c := range cases {
		s, err := Parse(c.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.expr, err)
			continue
		}
		if got := Describe(s.Spec); got != c.want {
			t.Errorf("Describe(%q)\n got  %q\n want %q", c.expr, got, c.want)
		}
	}
}

func TestOrdinal(t *testing.T) {
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st"} {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}
