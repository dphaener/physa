package mapfile

import (
	"regexp"
	"strings"
	"time"
)

var (
	slugSegRe = regexp.MustCompile("^`([^`]*)`$")
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	prRe      = regexp.MustCompile(`^#\d+$`)
	commitRe  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Order of the meta line's segments after the slug.
const (
	atPerson = iota + 1
	atDate
	atAnchors
)

// meta parses an entry's meta line into e. Segments must come in the order
// slug, person, date, anchors.
func (r *reader) meta(p string, no int, line string, e *Entry) {
	segs := strings.Split(line, " · ")
	m := slugSegRe.FindStringSubmatch(segs[0])
	if m == nil || !slugRe.MatchString(m[1]) {
		r.add(p, no, "meta line must start with a `slug` of lowercase words joined by hyphens")
	} else {
		e.Slug, e.MetaHas.Slug = m[1], true
	}
	stage := 0
	for _, seg := range segs[1:] {
		seg = strings.TrimSpace(seg)
		var at int
		switch {
		case strings.HasPrefix(seg, "decided by "), strings.HasPrefix(seg, "raised by "):
			at = atPerson
			r.person(p, no, seg, e)
		case dateRe.MatchString(seg):
			at = atDate
			if _, err := time.Parse("2006-01-02", seg); err != nil {
				r.add(p, no, "%q is not a real date", seg)
			}
			e.Date, e.MetaHas.Date = seg, true
		case strings.HasPrefix(seg, "from "), anchorTokens(seg):
			at = atAnchors
			r.anchors(p, no, seg, e)
		default:
			r.add(p, no, "unknown meta segment %q: want a person, a date or anchors", seg)
			continue
		}
		if at < stage || (at == stage && at != atAnchors) {
			r.add(p, no, "meta segment %q is out of order: want slug, person, date, anchors", seg)
		}
		stage = max(stage, at)
	}
}

func anchorTokens(seg string) bool {
	toks := strings.Fields(seg)
	for _, t := range toks {
		if !prRe.MatchString(t) && !commitRe.MatchString(t) {
			return false
		}
	}
	return len(toks) > 0
}

func (r *reader) person(p string, no int, seg string, e *Entry) {
	role, rest, _ := strings.Cut(seg, " by ")
	name, how, _ := strings.Cut(rest, ",")
	name, how = strings.TrimSpace(name), strings.TrimSpace(how)
	if name == "" {
		r.add(p, no, "%q names no person", seg)
	}
	e.Person, e.How = name, how
	if role == "decided" || role == "raised" {
		e.Role = role
	}
	e.MetaHas.Person = true
}

func (r *reader) anchors(p string, no int, seg string, e *Entry) {
	e.MetaHas.Anchors = true
	if rest, ok := strings.CutPrefix(seg, "from "); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" || strings.ContainsAny(rest, " \t") {
			r.add(p, no, "from must be followed by one path without spaces")
			return
		}
		e.Anchors = append(e.Anchors, Anchor{Doc, rest})
		return
	}
	for _, t := range strings.Fields(seg) {
		if prRe.MatchString(t) {
			e.Anchors = append(e.Anchors, Anchor{PR, t[1:]})
		} else {
			e.Anchors = append(e.Anchors, Anchor{Commit, t})
		}
	}
}
