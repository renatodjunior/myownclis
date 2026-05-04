package cmd

import "testing"

func TestExpandCronAlias(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"* * * * *", "* * * * *"},
		{"@daily", "@daily"},
		{"every minute", "* * * * *"},
		{"minutely", "* * * * *"},
		{"every 5m", "*/5 * * * *"},
		{"every 15 minutes", "*/15 * * * *"},
		{"5m", "*/5 * * * *"},
		{"hourly", "0 * * * *"},
		{"every hour", "0 * * * *"},
		{"every 2h", "0 */2 * * *"},
		{"2h", "0 */2 * * *"},
		{"daily", "0 0 * * *"},
		{"every day", "0 0 * * *"},
		{"daily at 9am", "0 9 * * *"},
		{"daily at 9", "0 9 * * *"},
		{"daily 14:30", "30 14 * * *"},
		{"daily at 12am", "0 0 * * *"},
		{"daily at 12pm", "0 12 * * *"},
		{"daily at 3pm", "0 15 * * *"},
		{"weekdays", "0 9 * * 1-5"},
		{"weekdays at 8am", "0 8 * * 1-5"},
		{"weekends", "0 9 * * 0,6"},
		{"weekends at 10:30am", "30 10 * * 0,6"},
		// pass-through for unknown forms
		{"bogus", "bogus"},
	}
	for _, c := range cases {
		got := ExpandCronAlias(c.in)
		if got != c.want {
			t.Errorf("ExpandCronAlias(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
