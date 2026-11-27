package sourceset

import "sort"

// Claim is the path patterns one owner says are its own.
type Claim struct {
	Owner    string
	Patterns []string
}

// Claims resolves which owner each file goes to: the owner whose pattern on
// it is most specific. When two different owners tie at the highest
// specificity the file is claimed twice: it is in twice with its owners
// sorted, and in no owner of where. A file no pattern matches is missing.
// Files keep their input order.
func Claims(files []string, claims []Claim) (where map[string]string, missing []string, twice map[string][]string) {
	where = map[string]string{}
	twice = map[string][]string{}
	for _, f := range files {
		top := 0
		var owners []string
		for _, c := range claims {
			s := best(c.Patterns, f)
			switch {
			case s == 0:
			case s > top:
				top, owners = s, []string{c.Owner}
			case s == top && !has(owners, c.Owner):
				owners = append(owners, c.Owner)
			}
		}
		switch len(owners) {
		case 0:
			missing = append(missing, f)
		case 1:
			where[f] = owners[0]
		default:
			sort.Strings(owners)
			twice[f] = owners
		}
	}
	return where, missing, twice
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
