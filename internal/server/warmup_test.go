package server

import "testing"

func TestFeedProblem(t *testing.T) {
	cases := []struct {
		body string
		bad  bool
	}{
		{`{"value":45,"classification":"Fear"}`, false},
		{`{"value":null,"classification":"","error":"http 418: You're a bot"}`, true},
		{`{"value":null}`, true},
		{`{"articles":[{"title":"x"}],"source":"rss"}`, false},
		{`{"articles":[],"source":"unconfigured"}`, true},
		{`{"articles":[],"source":"rss"}`, true},
		{`not json`, true},
	}
	for _, c := range cases {
		if got := feedProblem([]byte(c.body)) != ""; got != c.bad {
			t.Errorf("%s: problem=%v, want %v", c.body, got, c.bad)
		}
	}
}
