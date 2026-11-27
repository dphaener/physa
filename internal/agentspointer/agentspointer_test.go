package agentspointer

import "testing"

func TestWithKeepsEveryByteOutsideTheMarkers(t *testing.T) {
	cases := []struct {
		name, before, want string
	}{
		{"with markers", "# Agents\n\nintro\n<!-- physa -->\nold pointer\n<!-- /physa -->\ntail\nmore\n",
			"# Agents\n\nintro\n" + Pointer + "\ntail\nmore\n"},
		{"without markers", "# Agents\n\nRules here.\n", "# Agents\n\nRules here.\n\n" + Pointer + "\n"},
		{"without markers or last newline", "# Agents", "# Agents\n\n" + Pointer + "\n"},
		{"absent file", "", Pointer + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := With(c.before); got != c.want {
				t.Errorf("With =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}
