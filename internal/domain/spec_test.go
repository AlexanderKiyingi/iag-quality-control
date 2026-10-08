package domain

import "testing"

func f(v float64) *float64 { return &v }

func TestWithinBand(t *testing.T) {
	cases := []struct {
		name     string
		v        float64
		lsl, usl *float64
		want     bool
	}{
		{"upper only, inside", 12.0, nil, f(12.5), true},
		{"upper only, on the limit is inside", 12.5, nil, f(12.5), true},
		{"upper only, outside", 12.6, nil, f(12.5), false},
		{"lower only, outside", 9.9, f(10), nil, false},
		{"two-sided, inside", 11, f(10), f(12), true},
		{"two-sided, below", 9, f(10), f(12), false},
		{"exact-match band", 1, f(1), f(1), true},
	}
	for _, tc := range cases {
		if got := WithinBand(tc.v, tc.lsl, tc.usl); got != tc.want {
			t.Errorf("%s: WithinBand(%v) = %v, want %v", tc.name, tc.v, got, tc.want)
		}
	}
}

func TestCombineVerdicts(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want string
	}{
		"any fail fails":            {[]string{VerdictPass, VerdictFail}, VerdictFail},
		"pass beats no spec":        {[]string{VerdictNoSpec, VerdictPass}, VerdictPass},
		"not numeric beats no spec": {[]string{VerdictNoSpec, VerdictNotNumeric}, VerdictNotNumeric},
		"nothing evaluated":         {nil, ""},
	}
	for name, tc := range cases {
		if got := CombineVerdicts(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestContradictsVerdict(t *testing.T) {
	cases := []struct {
		result, verdict string
		want            bool
	}{
		{"pass", VerdictFail, true},
		{"accept", VerdictFail, true},
		{"in_control", VerdictFail, true},
		{"fail", VerdictPass, true},
		{"quarantine", VerdictPass, true},
		{"reject", VerdictFail, false},
		{"pending", VerdictFail, false},
		{"", VerdictFail, false},
		{"pass", VerdictNoSpec, false},
		{"fail", VerdictNotNumeric, false},
	}
	for _, tc := range cases {
		if got := ContradictsVerdict(tc.result, tc.verdict); got != tc.want {
			t.Errorf("ContradictsVerdict(%q, %q) = %v, want %v", tc.result, tc.verdict, got, tc.want)
		}
	}
}
