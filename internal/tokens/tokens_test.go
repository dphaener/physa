package tokens

import "testing"

func TestParse(t *testing.T) {
	good := map[string]int64{
		"200k": 200_000, "200K": 200_000, "1.5m": 1_500_000, "2M": 2_000_000,
		"1,500,000": 1_500_000, "1_000": 1000, "42": 42, "0.5k": 500, "2.25k": 2250,
		" 3m ": 3_000_000,
	}
	for in, want := range good {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0k", "-5", "k", "1.5", "1.2345k", "1.2.3m", "abc", "1x", "99999999999999999999", "9999999999999m"} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %d, want error", in, got)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := map[int64]string{
		200_000: "200k", 1_500_000: "1.5m", 2_000_000: "2m", 1_050_000: "1050k",
		1234: "1234", 500: "500", 1000: "1k", 999_999: "999999",
	}
	for n, want := range cases {
		if got := Format(n); got != want {
			t.Errorf("Format(%d) = %q, want %q", n, got, want)
		}
		if back, err := Parse(Format(n)); err != nil || back != n {
			t.Errorf("Format(%d) does not parse back: %d, %v", n, back, err)
		}
	}
}
