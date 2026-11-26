package shares

import (
	"errors"
	"fmt"

	"github.com/dphaener/physa/internal/reading"
)

// LayCapability lays one capability's sources in for recovery (design 02
// §2.4): as LayFor does, into shares whose messages, as message writes
// them, each take at most room estimated tokens, except the docs every
// capability reads (reading.Always), which recovery's sessions read in their
// system prompt: they are not laid, and every share keeps them. A source too
// large for one share is an error naming it.
func LayCapability(in reading.Input, room int, message func(reading.Input) string) ([]reading.Input, error) {
	var always, own []reading.Doc
	for _, d := range in.Docs {
		if reading.Always(d.Kind) {
			always = append(always, d)
		} else {
			own = append(own, d)
		}
	}
	in.Docs = own
	parts, err := LayFor(in, room, message)
	if tl := (*TooLarge)(nil); errors.As(err, &tl) {
		return nil, fmt.Errorf("%s takes about %s estimated tokens, more than one share of this capability's sources holds (about %s); nothing is cut, so the capability is not recovered",
			tl.Source, reading.Thousands(int64(tl.Tokens)), reading.Thousands(int64(tl.Room)))
	}
	for i := range parts {
		parts[i].Docs = append(append([]reading.Doc{}, always...), parts[i].Docs...)
	}
	return parts, err
}

// JoinCapability is the sources of parts, laid by LayCapability, together
// again in order, the docs every capability reads once, under whole's
// Report; with no parts it is whole.
func JoinCapability(parts []reading.Input, whole reading.Input) reading.Input {
	if len(parts) == 0 {
		return whole
	}
	out := reading.Input{Report: whole.Report}
	for i, p := range parts {
		for _, d := range p.Docs {
			if i == 0 || !reading.Always(d.Kind) {
				out.Docs = append(out.Docs, d)
			}
		}
		out.PRs = append(out.PRs, p.PRs...)
		out.Commits = append(out.Commits, p.Commits...)
	}
	return out
}
