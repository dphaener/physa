package reading

import "fmt"

// Report says what a session reads: one SourceReport per source, in order
// (docs, pull requests, review comments, changes), and their estimated
// tokens together.
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

// sourceNames are the sources a Report counts, in order.
var sourceNames = []string{"docs", "pull requests", "review comments", "changes"}

// Recounted is in with its Report counted again from what in holds, under
// the sources in's Report names (all of sourceNames when it names none). A
// part of an Input, such as one share of a capability's sources, so reports
// exactly what it reads, counted as Of counts it.
func (in Input) Recounted() Input {
	var names []string
	for _, s := range in.Report.Sources {
		names = append(names, s.Name)
	}
	if names == nil {
		names = sourceNames
	}
	in.Report = Report{}
	for _, n := range names {
		s := SourceReport{Name: n}
		switch n {
		case "docs":
			for _, d := range in.Docs {
				s.add(len(d.Path) + len(d.Kind) + len(d.Body))
			}
		case "pull requests":
			for _, p := range in.PRs {
				b := len(p.Title) + len(p.Author) + len(p.Body)
				for _, is := range p.Issues {
					b += len(is)
				}
				s.add(b)
			}
		case "review comments":
			for _, p := range in.PRs {
				for _, c := range p.Comments {
					s.add(len(c.Author) + len(c.Body))
				}
			}
		case "changes":
			for _, k := range in.Commits {
				s.add(len(k.Short) + len(k.Subject) + len(k.Message) + len(k.Author) + len(k.Reverts))
			}
		}
		s.Tokens = BytesTokens(s.bytes)
		in.Report.Tokens += s.Tokens
		in.Report.Sources = append(in.Report.Sources, s)
	}
	return in
}
