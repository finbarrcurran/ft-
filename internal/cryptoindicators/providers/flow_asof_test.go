package providers

import "testing"

func TestEffectiveAsOfTrailingZeroRule(t *testing.T) {
	cases := []struct {
		name string
		rows []FlowRow
		want string
	}{
		{"zero placeholder dated today, previous day real", []FlowRow{{"2026-10-13", 0}, {"2026-10-12", 55.5}, {"2026-10-09", 21.1}}, "2026-10-12"},
		{"zero yesterday, then non-zero, but a real newest row", []FlowRow{{"2026-10-13", 40}, {"2026-10-12", 0}, {"2026-10-09", 21.1}}, "2026-10-13"},
		{"legitimate non-zero newest row", []FlowRow{{"2026-10-12", -244.1}, {"2026-10-09", 21.1}}, "2026-10-12"},
		{"two zeros in a row: not a single placeholder, keep the newest", []FlowRow{{"2026-10-13", 0}, {"2026-10-12", 0}, {"2026-10-09", 5}}, "2026-10-13"},
		{"unordered input", []FlowRow{{"2026-10-09", 21.1}, {"2026-10-13", 0}, {"2026-10-12", 7}}, "2026-10-12"},
		{"single zero row only", []FlowRow{{"2026-10-13", 0}}, "2026-10-13"},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := EffectiveAsOf(c.rows); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
