package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Proposal is one map entry the session proposes, as the learning skill's
// output contract writes it. Parse checks only its shape; whether it holds
// against the batch and the map is the next stage's to say.
type Proposal struct {
	// Kind is rule, decision, question or promise.
	Kind string `json:"kind"`
	// Capability is the capability whose file the entry goes in.
	Capability string `json:"capability"`
	// Section is Rules, Decisions, Questions or promise.
	Section string `json:"section"`
	// Signal names the signal of the batch the proposal comes from.
	Signal SignalRef `json:"signal"`
	// Changes are the short ids of the changes the entry is anchored to.
	Changes []string `json:"changes"`
	// Entry is the entry in the map's form, or the new promise.
	Entry string `json:"entry"`
}

// SignalRef names a signal of the batch by its kind and the changes it is
// about.
type SignalRef struct {
	Kind    string   `json:"kind"`
	Changes []string `json:"changes"`
}

// Malformed is one item of the session's output that is not a proposal, kept
// so that it is reported and never dropped silently.
type Malformed struct {
	// Raw is the item as the session wrote it.
	Raw string
	// Reason says what is wrong with it.
	Reason string
}

// Parse reads the session's answer: the last fenced json block in text,
// holding one object with a proposals list. The error, worded to follow "the
// session's output: ", is for an answer with no such block or one that does
// not decode. Each item is decoded on its own, refusing unknown keys, so one
// that is not a proposal becomes a Malformed and the others are kept, in
// order.
func Parse(text string) ([]Proposal, []Malformed, error) {
	block, ok := lastJSONBlock(text)
	if !ok {
		return nil, nil, errors.New("it has no fenced json block")
	}
	var w struct {
		Proposals *[]json.RawMessage `json:"proposals"`
	}
	dec := json.NewDecoder(strings.NewReader(block))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return nil, nil, errors.New("its json block does not decode: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("its json block holds more than one value")
	}
	if w.Proposals == nil {
		return nil, nil, errors.New("its json block has no proposals list: write [] for an empty one")
	}
	var (
		proposals []Proposal
		malformed []Malformed
	)
	for _, raw := range *w.Proposals {
		p, err := proposal(raw)
		if err != nil {
			malformed = append(malformed, Malformed{Raw: string(raw), Reason: err.Error()})
			continue
		}
		proposals = append(proposals, p)
	}
	return proposals, malformed, nil
}

// proposal decodes one item of the proposals list.
func proposal(raw json.RawMessage) (Proposal, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return Proposal{}, errors.New("a proposal must be a JSON object")
	}
	var p Proposal
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// lastJSONBlock returns the content of the last closed fence opened by a line
// of three or more backticks followed by json.
func lastJSONBlock(text string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var (
		found  string
		ok     bool
		open   string // the opening fence's backticks; empty outside a fence
		isJSON bool
		start  int
	)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if open == "" {
			ticks := len(t) - len(strings.TrimLeft(t, "`"))
			if ticks >= 3 {
				open, start = t[:ticks], i+1
				isJSON = strings.EqualFold(strings.TrimSpace(t[ticks:]), "json")
			}
			continue
		}
		if strings.HasPrefix(t, open) && strings.Trim(t, "`") == "" {
			if isJSON {
				found, ok = strings.Join(lines[start:i], "\n"), true
			}
			open = ""
		}
	}
	return found, ok
}
