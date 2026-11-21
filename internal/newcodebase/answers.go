package newcodebase

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// The answers file read by ReadAnswers has one answer per line, as
// `1: <answer>` to `3: <answer>`, numbered as Questions numbers them. Blank
// lines and lines starting with `#` are ignored. A number left out takes the
// suggestion of the questions passed in; with none to take, the name and the
// promise are errors and the terms are none. The first colon ends the number,
// so a term's own colon stays in the answer, as in `3: Cut list: The parts`.

// Term is one term of the glossary with its meaning.
type Term struct {
	Name, Meaning string
}

// Answers is what the person said: the product's name and promise, and its
// first terms.
type Answers struct {
	Name, Promise string
	Terms         []Term
}

// GlossaryTerms renders the terms as glossary.md wants them (design 01 §4):
// `**Name**: meaning`, one per paragraph.
func (a Answers) GlossaryTerms() string {
	parts := make([]string, len(a.Terms))
	for i, t := range a.Terms {
		parts[i] = "**" + t.Name + "**: " + t.Meaning
	}
	return strings.Join(parts, "\n\n")
}

// maxAsks is how many times Ask asks a question again after a bad answer.
const maxAsks = 2

// check validates the answer text of question n and stores it in a.
func (a *Answers) check(n int, text string) error {
	text = strings.TrimSpace(text)
	switch n {
	case 1:
		if text == "" || strings.ContainsAny(text, "\r\n") {
			return fmt.Errorf("question 1: the name must be one non-empty line")
		}
		a.Name = text
	case 2:
		if text == "" {
			return fmt.Errorf("question 2: the promise is empty")
		}
		if strings.Contains(text, "…") {
			return fmt.Errorf("question 2: the promise still holds …; write it in your own words")
		}
		a.Promise = text
	case 3:
		terms, err := parseTerms(text)
		if err != nil {
			return fmt.Errorf("question 3: %w", err)
		}
		a.Terms = terms
	}
	return nil
}

func parseTerms(text string) ([]Term, error) {
	if t := strings.ToLower(text); t == "" || t == "none" || t == noTerms {
		return nil, nil
	}
	var terms []Term
	for _, part := range strings.Split(text, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, meaning, ok := strings.Cut(part, ":")
		name, meaning = strings.TrimSpace(name), strings.TrimSpace(meaning)
		if !ok || name == "" || meaning == "" {
			return nil, fmt.Errorf("term %q needs the form Name: meaning", part)
		}
		terms = append(terms, Term{name, meaning})
	}
	return terms, nil
}

// Ask prints each question as `1. <question>` and `   Suggested: <answer>`,
// and reads one line. An empty line or `yes` accepts the suggestion. An
// answer that fails its check (a promise still holding …, a term without a
// meaning) is asked again, at most twice, then it is an error. Input that
// ends before an answer is an error: Ask never answers for the person.
func Ask(in io.Reader, out io.Writer, qs []Question) (Answers, error) {
	r := bufio.NewReader(in)
	var a Answers
	for _, q := range qs {
		for try := 0; ; try++ {
			fmt.Fprintf(out, "%d. %s\n   Suggested: %s\n> ", q.Number, q.Text, q.Suggested)
			line, err := r.ReadString('\n')
			if err != nil && line == "" {
				return a, fmt.Errorf("question %d: no answer: input ended", q.Number)
			}
			text := strings.TrimSpace(line)
			if text == "" || strings.EqualFold(text, "yes") {
				text = q.Suggested
			}
			err = a.check(q.Number, text)
			if err == nil {
				break
			}
			fmt.Fprintf(out, "   %v\n", err)
			if try == maxAsks {
				return a, fmt.Errorf("%w (asked %d times)", err, try+1)
			}
		}
	}
	return a, nil
}

// ReadAnswers reads the answers file at path, in the format above. Missing
// numbers take the suggestions in qs.
func ReadAnswers(path string, qs ...Question) (Answers, error) {
	f, err := os.Open(path)
	if err != nil {
		return Answers{}, err
	}
	defer f.Close()
	texts := map[int]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		num, text, ok := strings.Cut(line, ":")
		q, qerr := strconv.Atoi(strings.TrimSpace(num))
		if !ok || qerr != nil || q < 1 || q > 3 {
			return Answers{}, fmt.Errorf("%s line %d: want `1: <answer>` to `3: <answer>`", path, n)
		}
		if _, dup := texts[q]; dup {
			return Answers{}, fmt.Errorf("%s line %d: question %d is answered twice", path, n, q)
		}
		texts[q] = text
	}
	if err := sc.Err(); err != nil {
		return Answers{}, err
	}
	var a Answers
	for q := 1; q <= 3; q++ {
		text, ok := texts[q]
		if !ok {
			for _, s := range qs {
				if s.Number == q {
					text = s.Suggested
				}
			}
		}
		if err := a.check(q, text); err != nil {
			return Answers{}, fmt.Errorf("%s: %w", path, err)
		}
	}
	return a, nil
}
