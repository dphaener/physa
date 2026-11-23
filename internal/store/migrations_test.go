package store

import (
	"strings"
	"testing"
)

func TestEmbeddedMigrationsAreNumberedOnce(t *testing.T) {
	if _, err := migrations(); err != nil {
		t.Fatal(err)
	}
}

func TestOrderMigrations(t *testing.T) {
	ms, err := orderMigrations([]string{"0002_b.sql", "0001_a.sql", "0003_c.sql"})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"0001_a.sql", "0002_b.sql", "0003_c.sql"} {
		if ms[i].name != want || ms[i].n != i+1 {
			t.Errorf("ms[%d] = %+v, want %s", i, ms[i], want)
		}
	}
}

func TestOrderMigrationsRefuses(t *testing.T) {
	cases := map[string]struct {
		names []string
		want  string
	}{
		"shared number": {[]string{"0001_a.sql", "0002_b.sql", "0002_c.sql"}, `migrations "0002_b.sql" and "0002_c.sql" share number 2`},
		"gap":           {[]string{"0001_a.sql", "0003_c.sql"}, `migration number 2 is missing (next is "0003_c.sql")`},
		"no first":      {[]string{"0002_b.sql"}, "migration number 1 is missing"},
		"bad name":      {[]string{"first.sql"}, "is not named <number>_<name>.sql"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := orderMigrations(c.names)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}
