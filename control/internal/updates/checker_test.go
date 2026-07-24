package updates

import "testing"

func TestNewer(t *testing.T) {
	for _, test := range []struct {
		candidate, current string
		want               bool
	}{
		{"0.2.0", "0.1.9", true}, {"0.1.1", "0.1.0", true}, {"0.1.0", "0.1.0-rc.3", true},
		{"0.1.0-rc.4", "0.1.0-rc.3", true}, {"0.1.0-rc.2", "0.1.0-rc.3", false}, {"0.1.0", "0.1.0", false}, {"bad", "0.1.0", false},
	} {
		if got := newer(test.candidate, test.current); got != test.want {
			t.Errorf("newer(%q, %q)=%v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}
