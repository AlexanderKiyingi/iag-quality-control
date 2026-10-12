package domain

import "testing"

func TestCodeLetter(t *testing.T) {
	cases := []struct {
		lot   int
		level string
		want  string
	}{
		{8, "II", "A"}, {9, "II", "B"}, {500, "II", "H"}, {501, "II", "J"}, {1200, "II", "J"},
		{3201, "II", "L"}, {500001, "II", "Q"}, {500001, "III", "R"}, {1000, "I", "G"}, {1000, "S-4", "F"}, {100, "S-1", "B"},
	}
	for _, c := range cases {
		if got, err := CodeLetter(c.lot, c.level); err != nil || got != c.want {
			t.Errorf("lot %d level %s: got %s (%v) want %s", c.lot, c.level, got, err, c.want)
		}
	}
	if _, err := CodeLetter(100, "IV"); err == nil {
		t.Error("level IV does not exist")
	}
}

func TestTable2A(t *testing.T) {
	// Published single-sampling normal-inspection cells.
	cases := []struct {
		letter    string
		aql       float64
		n, ac, re int
		used      string
	}{
		{"J", 1.0, 80, 2, 3, "J"},
		{"J", 2.5, 80, 5, 6, "J"},
		{"J", 0.65, 80, 1, 2, "J"},
		{"J", 0.15, 80, 0, 1, "J"},
		{"G", 1.5, 32, 1, 2, "G"},
		{"G", 4.0, 32, 3, 4, "G"},
		{"G", 10, 32, 7, 8, "G"},
		{"K", 0.10, 125, 0, 1, "K"},
		{"L", 2.5, 200, 10, 11, "L"},
		{"A", 6.5, 2, 0, 1, "A"},
		{"Q", 0.010, 1250, 0, 1, "Q"},
		// Arrows: J at 0.25 points up to H's 0/1; J at 0.40 down to K's 1/2.
		{"J", 0.25, 50, 0, 1, "H"},
		{"J", 0.40, 125, 1, 2, "K"},
		// Below the Ac 0 diagonal, down to the first plan.
		{"C", 0.65, 20, 0, 1, "F"},
	}
	for _, c := range cases {
		n, ac, re, used, err := Plan(c.letter, c.aql)
		if err != nil || n != c.n || ac != c.ac || re != c.re || used != c.used {
			t.Errorf("%s @ %g: got n=%d %d/%d via %s (%v); want n=%d %d/%d via %s",
				c.letter, c.aql, n, ac, re, used, err, c.n, c.ac, c.re, c.used)
		}
	}
	if _, _, _, _, err := Plan("J", 0.3); err == nil {
		t.Error("0.3 is not a column")
	}
}

func TestNewSamplingPlan(t *testing.T) {
	p, err := NewSamplingPlan(1000, "", 2.5)
	if err != nil || p.CodeLetter != "J" || p.SampleSize != 80 || p.Accept != 5 || p.Level != "II" {
		t.Fatalf("1000 bags at 2.5: %+v %v", p, err)
	}
	if p.Judge(5) != VerdictPass || p.Judge(6) != VerdictFail {
		t.Fatal("5 accepts, 6 rejects")
	}
	small, err := NewSamplingPlan(6, "II", 1.0)
	if err != nil || !small.HundredPercent || small.SampleSize != 6 {
		t.Fatalf("a lot smaller than its sample is inspected whole: %+v %v", small, err)
	}
}
