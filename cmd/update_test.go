package cmd

import "testing"

func TestNewerThanReadsDottedReleases(t *testing.T) {
	for _, c := range []struct {
		release, than string
		want          bool
	}{
		{"0.16.0", "0.15.0", true},
		{"0.15.1", "0.15.0", true},
		{"1.0.0", "0.15.0", true},
		{"0.13.0", "0.15.0", false},
		{"0.15.0", "0.15.0", false},
		{"", "0.15.0", false},
		{"0.9.0", "0.10.0", false},
	} {
		if got := newerThan(c.release, c.than); got != c.want {
			t.Errorf("newerThan(%q, %q) = %v, want %v", c.release, c.than, got, c.want)
		}
	}
}
