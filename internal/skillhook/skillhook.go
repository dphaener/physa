// Package skillhook is Physa's session hook in a hooks file of Claude Code's
// settings shape, which Claude Code's settings.json and Codex's hooks.json
// share: one SessionStart entry that runs "physa skill session" at the start of every
// session, and whether the entry at a path is current, stale, missing or not
// safe to edit. It writes and removes only Physa's entry. Every other key,
// hook event and SessionStart entry keeps its value and its order, and is
// never decoded into Go values; only whitespace may change. It never follows
// a symlinked settings file, and never edits JSON it cannot read.
package skillhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Command is the command Physa's entry runs. An entry is recognised as
// Physa's by it, since Claude Code's settings carry no marker.
const Command = "physa skill session"

// entry is Physa's SessionStart entry as this Physa writes it, in compact
// form: no matcher, so every session start runs it.
const entry = `{"hooks":[{"type":"command","command":"` + Command + `","timeout":5}]}`

// ErrUnsafe is returned when the settings file is a symlink, not a regular
// file, not valid JSON, or holds a hooks or SessionStart of another JSON
// type, so it is left alone.
var ErrUnsafe = errors.New("settings file Physa cannot edit safely")

// State is what a settings file holds of Physa's entry.
type State int

const (
	// Missing means no file, no hooks, no SessionStart, or no Physa entry.
	Missing State = iota
	// Current means one Physa entry, exactly as this Physa writes it.
	Current
	// Stale means a Physa entry that differs, or more than one.
	Stale
	// Other means a file Physa cannot edit safely.
	Other
)

// String names the state as the commands print it.
func (s State) String() string {
	switch s {
	case Missing:
		return "missing"
	case Current:
		return "current"
	case Stale:
		return "stale"
	default:
		return "other"
	}
}

// member is one key of a JSON object: the key exactly as written, its
// decoded name, and its value exactly as written.
type member struct {
	key   []byte
	name  string
	value json.RawMessage
}

// doc is a settings file as read: its members in order, the hooks object's
// members and the SessionStart entries when present, and the file's mode.
type doc struct {
	top      []member
	hooks    []member // nil when there is no hooks key
	hooksAt  int      // index of hooks in top, or -1
	starts   []json.RawMessage
	startsAt int // index of SessionStart in hooks, or -1
	mode     os.FileMode
}

// Read says what the settings file at path holds of Physa's entry. It never
// follows a symlink at path.
func Read(path string) (State, error) {
	_, st, err := load(path)
	return st, err
}

// load reads and parses the file at path. A nil doc with Missing means there
// is no file.
func load(path string) (*doc, State, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Missing, nil
	}
	if err != nil {
		return nil, Other, err
	}
	if !fi.Mode().IsRegular() {
		return nil, Other, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, Other, err
	}
	d, ok := parse(b)
	if !ok {
		return nil, Other, nil
	}
	d.mode = fi.Mode().Perm()
	return d, d.state(), nil
}

// parse splits a settings file into the parts Physa edits. It reports false
// for invalid JSON, a top level that is not an object, a hooks that is not an
// object or a SessionStart that is not an array, or either key repeated.
func parse(b []byte) (*doc, bool) {
	top, ok := object(b)
	if !ok {
		return nil, false
	}
	d := &doc{top: top, hooksAt: -1, startsAt: -1}
	i, n := find(top, "hooks")
	if n > 1 {
		return nil, false
	}
	if n == 0 {
		return d, true
	}
	d.hooksAt = i
	hooks, ok := object(top[i].value)
	if !ok {
		return nil, false
	}
	d.hooks = hooks
	j, n := find(hooks, "SessionStart")
	if n > 1 {
		return nil, false
	}
	if n == 0 {
		return d, true
	}
	d.startsAt = j
	starts, ok := array(hooks[j].value)
	if !ok {
		return nil, false
	}
	d.starts = starts
	return d, true
}

// state classifies the SessionStart entries.
func (d *doc) state() State {
	n, current := 0, false
	for _, e := range d.starts {
		if physas(e) {
			n++
			current = canonical(e)
		}
	}
	switch {
	case n == 0:
		return Missing
	case n == 1 && current:
		return Current
	default:
		return Stale
	}
}

// physas reports whether a SessionStart entry is Physa's: an object whose
// hooks array holds exactly one command hook running Command.
func physas(e json.RawMessage) bool {
	ms, ok := object(e)
	if !ok {
		return false
	}
	i, n := find(ms, "hooks")
	if n != 1 {
		return false
	}
	hs, ok := array(ms[i].value)
	if !ok || len(hs) != 1 {
		return false
	}
	h, ok := object(hs[0])
	if !ok {
		return false
	}
	return str(h, "type") == "command" && str(h, "command") == Command
}

// canonical reports whether e, compacted, is byte-equal to the entry this
// Physa writes.
func canonical(e json.RawMessage) bool {
	var b bytes.Buffer
	if err := json.Compact(&b, e); err != nil {
		return false
	}
	return b.String() == entry
}

// str is the string value of the one key name in ms, or "" when it is
// missing, repeated or not a string.
func str(ms []member, name string) string {
	i, n := find(ms, name)
	if n != 1 {
		return ""
	}
	var s string
	if err := json.Unmarshal(ms[i].value, &s); err != nil {
		return ""
	}
	return s
}

// find returns the index of the first member named name and how many there
// are.
func find(ms []member, name string) (int, int) {
	at, n := -1, 0
	for i, m := range ms {
		if m.name == name {
			if n == 0 {
				at = i
			}
			n++
		}
	}
	return at, n
}

// object reads b, which must be exactly one JSON object, as its members in
// order, each key and value kept as written.
func object(b []byte) ([]member, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	ms := []member{}
	for dec.More() {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, ok := tok.(string)
		if !ok {
			return nil, false
		}
		seg := b[before:dec.InputOffset()]
		q := bytes.IndexByte(seg, '"')
		if q < 0 {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		ms = append(ms, member{key: seg[q:], name: name, value: v})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	return ms, end(dec)
}

// array reads b, which must be exactly one JSON array, as its elements in
// order, each kept as written.
func array(b []byte) ([]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, false
	}
	es := []json.RawMessage{}
	for dec.More() {
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		es = append(es, v)
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim(']') {
		return nil, false
	}
	return es, end(dec)
}

// end reports whether nothing but whitespace follows.
func end(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}

// Write puts Physa's current entry in the settings file at path. It replaces
// a stale entry where it stands, dropping any second one, or appends the
// entry at the end of SessionStart, creating SessionStart, hooks or the file
// when absent. A current entry leaves the file's bytes as they are; a file
// Physa cannot edit safely returns ErrUnsafe.
func Write(path string) error {
	d, st, err := load(path)
	if err != nil {
		return err
	}
	switch st {
	case Other:
		return ErrUnsafe
	case Current:
		return nil
	}
	if d == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return save(path, []byte(`{"hooks":{"SessionStart":[`+entry+`]}}`), 0o644)
	}
	starts := make([]json.RawMessage, 0, len(d.starts)+1)
	placed := false
	for _, e := range d.starts {
		if !physas(e) {
			starts = append(starts, e)
			continue
		}
		if !placed {
			starts = append(starts, json.RawMessage(entry))
			placed = true
		}
	}
	if !placed {
		starts = append(starts, json.RawMessage(entry))
	}
	return save(path, d.with(starts), d.mode)
}

// Remove takes every Physa entry out of the settings file at path, then an
// emptied SessionStart, then an emptied hooks. The file stays, even as {}.
// With no Physa entry, the file's bytes are left as they are; a file Physa
// cannot edit safely returns ErrUnsafe.
func Remove(path string) error {
	d, st, err := load(path)
	if err != nil {
		return err
	}
	switch st {
	case Other:
		return ErrUnsafe
	case Missing:
		return nil
	}
	starts := make([]json.RawMessage, 0, len(d.starts))
	for _, e := range d.starts {
		if !physas(e) {
			starts = append(starts, e)
		}
	}
	return save(path, d.with(starts), d.mode)
}

// with is the document with SessionStart's entries replaced by starts, in
// compact form apart from the raw values. An empty starts drops SessionStart,
// and then an empty hooks drops hooks.
func (d *doc) with(starts []json.RawMessage) []byte {
	var hooks []member
	if d.hooksAt >= 0 {
		hooks = append(hooks, d.hooks...)
	}
	if len(starts) == 0 {
		if d.startsAt >= 0 {
			hooks = append(hooks[:d.startsAt:d.startsAt], hooks[d.startsAt+1:]...)
		}
	} else {
		ss := member{key: []byte(`"SessionStart"`), name: "SessionStart", value: encodeArray(starts)}
		if d.startsAt >= 0 {
			hooks[d.startsAt] = ss
		} else {
			hooks = append(hooks, ss)
		}
	}
	top := append([]member(nil), d.top...)
	switch {
	case len(hooks) == 0 && d.hooksAt >= 0:
		top = append(top[:d.hooksAt:d.hooksAt], top[d.hooksAt+1:]...)
	case len(hooks) > 0 && d.hooksAt >= 0:
		top[d.hooksAt].value = encodeObject(hooks)
	case len(hooks) > 0:
		top = append(top, member{key: []byte(`"hooks"`), name: "hooks", value: encodeObject(hooks)})
	}
	return encodeObject(top)
}

// encodeObject joins members back into an object.
func encodeObject(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(m.key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// encodeArray joins elements back into an array.
func encodeArray(es []json.RawMessage) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, e := range es {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(e)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// save indents b at 2 spaces with a trailing newline and writes it to path
// atomically: a temp file in the same directory with the given mode, then a
// rename over path.
func save(path string, b []byte, mode os.FileMode) (err error) {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
