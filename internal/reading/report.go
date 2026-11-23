package reading

import "fmt"

// Report says what a session reads: one SourceReport per source, in order
// (docs, pull requests, review comments, changes, then clusters when read),
// and their estimated tokens together.
type Report struct {
	Tokens  int
	Sources []SourceReport
}

// SourceReport counts one source: the items read, all of them whole, and
// their estimated tokens, without what the prompt writes around them.
type SourceReport struct {
	Name   string
	Items  int
	Tokens int
	bytes  int
}

func (s *SourceReport) add(bytes int) {
	s.Items++
	s.bytes += bytes
}

// Lines says per source what is read, one plain line each, e.g.
// "docs: 9, whole, about 52,000 tokens"; a source with nothing gathered
// says "none".
func (r Report) Lines() []string {
	var out []string
	for _, s := range r.Sources {
		if s.Items == 0 {
			out = append(out, s.Name+": none")
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s, whole, about %s tokens", s.Name, Thousands(int64(s.Items)), Thousands(int64(s.Tokens))))
	}
	return out
}
