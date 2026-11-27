package consolidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recoverinput"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/repeats"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// reserved is the capability part of a product rule's id, which the guard
// gives the product's rules; a capability of that name is left as recovered.
const reserved = "product"

// missing is an input consolidation needs that is not stored yet.
type missing struct{ msg string }

func (m missing) Error() string { return m.msg }

// input is what one consolidation is made from.
type input struct {
	walkAt      time.Time
	promise     string // the product's promise, recoverinput.NoPromise when none is stated
	stated      bool
	rows        map[string]initrecord.Recovered // eligible rows, by capability
	caps        []capabilitytext.Capability     // their capabilities, in walk order
	left        []string                        // eligible capabilities consolidation leaves out
	renamed     []repeats.Rename
	fingerprint string
	refused     []consolidating.Refusal
}

// Load reads the stored consolidation, false when none is stored.
func Load(ctx context.Context, st *initrecord.Store) (Result, bool, error) {
	var r Result
	c, ok, err := st.Consolidated(ctx)
	if err == nil && ok {
		err = json.Unmarshal(c.Body, &r)
	}
	return r, ok && err == nil, err
}

// Current says whether a consolidation is stored and still matches what it
// was made from: the recovered rows of the walk eligible now, the walk and
// the product's promise. Any later recovery, of one more capability too,
// makes it stale.
func Current(ctx context.Context, st *initrecord.Store) (bool, error) {
	r, ok, err := Load(ctx, st)
	if err != nil || !ok {
		return false, err
	}
	in, err := read(ctx, st, "")
	if errors.As(err, new(missing)) {
		err = nil
	}
	return err == nil && in.fingerprint == r.Fingerprint, err
}

// read gathers the input from st: the rows recovered from the stored walk
// with status recovered and text, in walk order, each parsed with its area
// and footprint (root gives its line counts). A piece not stored yet is a
// missing error naming the phases to run first.
func read(ctx context.Context, st *initrecord.Store, root string) (input, error) {
	var in input
	wr, walked, err := walk.Load(ctx, st)
	if err != nil {
		return in, fmt.Errorf("the stored walk cannot be read: %w", err)
	}
	if !walked {
		return in, missing{"no walk stored yet; run physa init --walk, --place and --recover first"}
	}
	src, _, err := recoverinput.Listed(ctx, st, mapping.Proposal{}, false)
	if err == nil && !src.FromWalk {
		err = missing{"no placements stored yet; run physa init --place and --recover first"}
	}
	if err != nil {
		return in, err
	}
	if in.promise, in.stated, err = promise(ctx, st); err != nil {
		return in, err
	}
	rows, err := st.Recovered(ctx)
	if err != nil {
		return in, err
	}
	in.walkAt, in.rows = src.At, map[string]initrecord.Recovered{}
	for _, r := range rows {
		if src.Holds(r.ProposalAt) && r.Status == recovery.StatusRecovered && r.File != "" {
			in.rows[r.Capability] = r
		}
	}
	h := sha256.New()
	fmt.Fprintf(h, "walk %s\npromise %q\n", src.At.UTC().Format(time.RFC3339Nano), in.promise)
	for _, c := range recoverinput.Walk(root, wr, place.Record{}, store.Gather{}) {
		r, ok := in.rows[c.Name]
		if !ok {
			continue
		}
		fmt.Fprintf(h, "%q %q %q %q\n", r.Capability, r.Status, r.File, r.Terms)
		if c.Name == reserved {
			in.left = append(in.left, c.Name)
			in.refused = append(in.refused, consolidating.Refusal{Step: consolidating.StepGuard, Item: c.Name,
				Reason: "a capability named product would share its ids with the product's rules, so it is left as recovered"})
			continue
		}
		pc, err := capabilitytext.Parse(c.Name, capabilitytext.Area{Name: c.Area, Parent: c.Parent}, r.File, r.Terms)
		if err != nil {
			return in, fmt.Errorf("the stored recovery cannot be read: %w", err)
		}
		pc.Footprint = c.Footprint
		in.caps = append(in.caps, pc)
	}
	in.fingerprint = hex.EncodeToString(h.Sum(nil))
	if len(in.caps)+len(in.left) == 0 {
		return in, missing{"no capability recovered from the walk yet; run physa init --recover first"}
	}
	for _, n := range wr.Renamed {
		in.renamed = append(in.renamed, repeats.Rename{Area: n.Area, From: n.From, To: n.To})
	}
	return in, nil
}

// promise is the stored proposal's product promise, or
// recoverinput.NoPromise and false on the walk path with none stated.
func promise(ctx context.Context, st *initrecord.Store) (string, bool, error) {
	prop, ok, err := st.Proposed(ctx)
	var s mapping.Stored
	if err == nil && ok {
		err = json.Unmarshal(prop.Body, &s)
	}
	if p := draft.OneLine(s.Proposal.Product.Promise); err == nil && p != "" {
		return p, true, nil
	}
	return recoverinput.NoPromise, false, err
}

// result is the record of the consolidation out made from in.
func (in input) result(out consolidating.Output, rec consolidating.Record) Result {
	res := Result{Fingerprint: in.fingerprint, WalkAt: in.walkAt, ProductPromise: in.promise, Record: rec,
		Capabilities: []Capability{}, MergedAway: []MergedAway{}, ProductRules: []string{}}
	res.Before = Counts{Capabilities: len(in.caps)}
	for _, c := range in.caps {
		res.Before.Entries += len(c.Entries())
	}
	from := map[string][]string{}
	for _, m := range rec.Merged {
		for _, g := range m.Group {
			if g == m.Name {
				continue
			}
			from[m.Name] = append(from[m.Name], g)
			r := in.rows[g]
			res.MergedAway = append(res.MergedAway, MergedAway{Name: g, Into: m.Name, SessionID: r.SessionID, Dropped: dropped(r)})
		}
	}
	res.After = Counts{Capabilities: len(out.Capabilities), ProductRules: len(out.Rules)}
	for _, c := range out.Capabilities {
		res.After.Entries += len(c.Entries())
		if !slices.Contains(rec.Consolidated, c.Name) {
			res.Left = append(res.Left, c.Name)
			continue
		}
		res.Capabilities = append(res.Capabilities, Capability{Name: c.Name, Promise: c.Promise(), Area: c.Area,
			MergedFrom: append([]string{}, from[c.Name]...), Footprint: c.Footprint, File: c.File(), Terms: c.TermsText(),
			SessionID: in.rows[c.Name].SessionID})
	}
	res.Left = append(res.Left, in.left...)
	for _, r := range out.Rules {
		res.ProductRules = append(res.ProductRules, r.Text())
	}
	return res
}

// dropped is the entries r's recovery dropped, none when its details hold
// none or cannot be read.
func dropped(r initrecord.Recovered) []recovering.Dropped {
	var d struct{ Dropped []recovering.Dropped }
	_ = json.Unmarshal(r.Details, &d)
	return append([]recovering.Dropped{}, d.Dropped...)
}

// formCheck writes out as the map would hold it in a scratch folder, its
// capabilities' folders, the product's index and rules and the terms, and
// runs the map check on it for form only, `from` anchors resolving in root.
// Any problem is an error naming the first ones.
func formCheck(ctx context.Context, out consolidating.Output, promise, root string) error {
	dir, err := os.MkdirTemp("", "physa-consolidate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var terms []string
	for _, c := range out.Capabilities {
		f, err := c.Folder()
		if err == nil {
			err = f.Write(dir, mapfolder.CapabilityDir(c.Name))
		}
		if err != nil {
			return fmt.Errorf("capability %s: %w", c.Name, err)
		}
		if t := strings.TrimSpace(c.TermsText()); t != "" {
			terms = append(terms, t)
		}
	}
	front := "---\npromise: " + promise + "\n---"
	files := map[string]string{mapfolder.IndexFile: mapfolder.Index(front, out.Product, promise, nil)}
	if len(out.Rules) > 0 {
		files[mapfolder.IndexFile] = mapfolder.Index(front, out.Product, promise, []mapfolder.Kind{mapfolder.Rule})
		files[mapfolder.RulesFile] = consolidating.ProductRules(out.Product, out.Rules)
	}
	if err := (mapfolder.Folder{Files: files}).Write(dir, mapfolder.ProductDir); err != nil {
		return err
	}
	for _, f := range [][2]string{{".physa/README.md", "# The map\n\nA scratch copy of the consolidated map, made to check it.\n"},
		{recovering.GlossaryPath, recovering.Glossary(strings.Join(terms, "\n\n"))}} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f[0])), []byte(f[1]), 0o644); err != nil {
			return err
		}
	}
	res, err := mapcheck.CheckWith(ctx, dir, mapcheck.Options{FormOnly: true, DocRoot: root})
	if err != nil {
		return err
	}
	var probs []string
	for _, p := range res.Problems {
		probs = append(probs, fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.Message))
	}
	if len(probs) > 0 {
		return errors.New("the consolidated map fails the map check, so nothing is stored: " + strings.Join(probs[:min(len(probs), 5)], "; "))
	}
	return nil
}
