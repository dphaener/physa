package draft

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
)

// skill is the learning skill: what the session is told before the batch it
// drafts from.
//
//go:embed learning.md
var skill string

// none stands in a section of the prompt that holds nothing.
const none = "none\n"

// Prompt returns everything the session reads for one batch: the learning
// skill, then the index, the slices for the batch's capabilities with their
// decisions and questions, and the signals, each under its own heading. All
// that others wrote (change messages, capability names from trailers,
// evidence) sits in fenced blocks nothing inside can close. The same map and
// batch always give the same text.
func Prompt(m *mapfile.Map, b Batch) (string, error) {
	if m == nil {
		return "", errors.New("no map to draft against")
	}
	var w strings.Builder
	w.WriteString(strings.TrimRight(skill, "\n"))
	w.WriteString("\n\n## Index\n\n")
	if err := mapindex.Write(&w, mapindex.Build(m)); err != nil {
		return "", err
	}
	w.WriteString("\n## Slices\n\n")
	if len(b.Capabilities) == 0 {
		w.WriteString(none)
	} else {
		s, err := mapslice.Build(m, mapslice.Request{Capabilities: b.Capabilities, WithDecisions: true, WithQuestions: true})
		if err != nil {
			return "", err
		}
		s.Trailers = nil // how to anchor a commit is for the agent that writes a change
		if err := mapslice.WriteMD(&w, s); err != nil {
			return "", err
		}
	}
	w.WriteString("\n## Signals\n\n")
	if b.Empty() {
		w.WriteString(none)
	}
	q := &quoter{}
	for i, c := range b.Changes {
		if i > 0 {
			w.WriteString("\n")
		}
		fmt.Fprintf(&w, "### Change %d of %d\n\n", i+1, len(b.Changes))
		head := "change: " + OneLine(c.Short)
		if c.Author != "" {
			head += "\nauthor: " + OneLine(c.Author)
		}
		if !c.At.IsZero() {
			head += "\ndate: " + c.At.UTC().Format(time.RFC3339)
		}
		Fence(&w, head+"\nsubject: "+OneLine(c.Subject)+"\nmessage:\n"+strings.TrimSpace(c.Message))
		for j, s := range c.Signals {
			fmt.Fprintf(&w, "\nSignal %d of %d on this change:\n\n", j+1, len(c.Signals))
			var t strings.Builder
			t.WriteString("kind: " + OneLine(s.Kind) + "\n")
			t.WriteString("rule: " + OneLine(s.Rule) + "\n")
			t.WriteString("changes: " + OneLine(strings.Join(s.Changes, " ")) + "\n")
			if s.Capability != "" {
				t.WriteString("capability: " + OneLine(s.Capability) + "\n")
			}
			if s.Session != "" {
				t.WriteString("session: " + OneLine(first(s.Session, 8)) + "\n")
				t.WriteString("matched by: " + OneLine(s.MatchedBy) + "\n")
			}
			t.WriteString("evidence:")
			for _, e := range s.Evidence {
				t.WriteString("\n- " + strings.TrimSpace(e))
			}
			Fence(&w, t.String())
			q.quote(&w, s)
		}
	}
	return w.String(), nil
}

// first returns the first n characters of s.
func first(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Bounds on what the prompt quotes of others' words (design 08 section 7).
const (
	// commentRunes is the most characters quoted of one comment, of one
	// reverted change's message, and of one session quote's text or answer.
	commentRunes = 1500
	// batchRunes is the most characters of all such quotes in one batch.
	batchRunes = 20000
)

// quoter writes the text a signal quotes, a comment or a reverted message,
// within the bounds, and counts what it cut or left out.
type quoter struct {
	used    int  // characters quoted so far
	cut     int  // quotes cut to commentRunes
	leftOut int  // comments left out for the batch bound
	msgOut  int  // reverted messages left out for the batch bound
	quotes  int  // session quotes left out for the batch bound
	spent   bool // the batch bound is reached; nothing more is quoted
}

// quote writes what s quotes, each in its own fenced block, to w; a nil w
// only counts. A session quote is a whole or left out: its text and its
// answer are each bounded, and either passing the batch bound leaves it out.
func (q *quoter) quote(w *strings.Builder, s learn.Signal) {
	if w == nil {
		w = &strings.Builder{}
	}
	for i, cm := range s.Comments {
		text, n := q.take(cm.Body)
		if n < 0 {
			q.leftOut += len(s.Comments) - i
			fmt.Fprintf(w, "\n%d more comments left out to keep the batch within its bound\n", len(s.Comments)-i)
			break
		}
		fmt.Fprintf(w, "\nComment %d of %d on this signal, oldest first:\n\n", i+1, len(s.Comments))
		Fence(w, "author: "+OneLine(cm.Author)+"\nat: "+OneLine(cm.At.UTC().Format(time.RFC3339))+"\ntext:\n"+text)
	}
	for i, sq := range s.Quotes {
		text, n := q.take(sq.Text)
		answer := ""
		if n >= 0 && strings.TrimSpace(sq.Answer) != "" {
			answer, n = q.take(sq.Answer)
		}
		if n < 0 {
			q.quotes += len(s.Quotes) - i
			fmt.Fprintf(w, "\n%d more quotes left out to keep the batch within its bound\n", len(s.Quotes)-i)
			break
		}
		fmt.Fprintf(w, "\nQuote %d of %d on this signal:\n\n", i+1, len(s.Quotes))
		var t strings.Builder
		t.WriteString("author: " + OneLine(sq.Author) + "\nat: " + OneLine(sq.At.UTC().Format(time.RFC3339)) + "\ntext:\n" + text)
		if len(sq.Options) > 0 {
			t.WriteString("\noptions offered:")
			for _, o := range sq.Options {
				t.WriteString("\n- " + OneLine(o))
			}
		}
		if answer != "" {
			t.WriteString("\nanswer:\n" + answer)
		}
		Fence(w, t.String())
	}
	if s.Reverted != nil {
		text, n := q.take(s.Reverted.Message)
		w.WriteString("\nThe reverted change:\n\n")
		if n < 0 {
			q.msgOut++
			Fence(w, "subject: "+OneLine(s.Reverted.Subject))
			w.WriteString("\nits message is left out to keep the batch within its bound\n")
			return
		}
		Fence(w, "subject: "+OneLine(s.Reverted.Subject)+"\nmessage:\n"+text)
	}
}

// take returns body bounded: cut to commentRunes with the cut said, and n its
// length in characters, or -1 when the batch bound would be passed by it.
func (q *quoter) take(body string) (text string, n int) {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) > commentRunes {
		text = string(r[:commentRunes]) + fmt.Sprintf("\n(cut: %d of %d characters)", commentRunes, len(r))
		n = commentRunes
	} else {
		text, n = body, len(r)
	}
	if q.spent || q.used+n > batchRunes {
		q.spent = true
		return "", -1
	}
	q.used += n
	if len(r) > commentRunes {
		q.cut++
	}
	return text, n
}

// Cuts says in one clause what Prompt cut or left out of what it quotes of
// the batch, or returns "" when it cut nothing.
func (b Batch) Cuts() string {
	q := &quoter{}
	for _, c := range b.Changes {
		for _, s := range c.Signals {
			q.quote(nil, s)
		}
	}
	var parts []string
	if q.cut > 0 {
		parts = append(parts, fmt.Sprintf("%s cut to %d characters", plural(q.cut, "quote", "quotes"), commentRunes))
	}
	if q.leftOut > 0 {
		parts = append(parts, plural(q.leftOut, "comment", "comments") + " left out")
	}
	if q.quotes > 0 {
		parts = append(parts, plural(q.quotes, "session quote", "session quotes") + " left out")
	}
	if q.msgOut > 0 {
		parts = append(parts, plural(q.msgOut, "reverted message", "reverted messages") + " left out")
	}
	return strings.Join(parts, ", ")
}

// OneLine returns s with every run of white space as one space, so a value
// keeps to its own line of a fenced block.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Fence writes text in a fence longer than any run of backticks in it, so
// that nothing inside can close it.
func Fence(w io.Writer, text string) {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	fmt.Fprint(w, fence+"text\n")
	if text = strings.TrimRight(text, "\n"); text != "" {
		fmt.Fprint(w, text+"\n")
	}
	fmt.Fprint(w, fence+"\n")
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
