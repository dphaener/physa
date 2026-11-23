package workapi

import (
	"context"
	"log"

	"github.com/dphaener/physa/internal/api"
	observe "github.com/dphaener/physa/internal/evidence"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/state"
	stateevidence "github.com/dphaener/physa/internal/state/evidence"
	"github.com/dphaener/physa/internal/state/work"
)

// StateOptions registers the evidence poll and its retention in every repo's
// state, with GitHub as the forge, for the engine to run with.
func StateOptions() state.Options {
	timers, intents := stateevidence.Options(func(root string) forge.Forge { return github.New(root) })
	return state.Options{Timers: timers, Intents: intents}
}

// pursuitEvidenceMax caps the facts pursuit.show carries.
const pursuitEvidenceMax = 50

// shortID is the first eight characters of a commit id.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func evidenceOut(fs []observe.Fact) []EvidenceOut {
	out := make([]EvidenceOut, 0, len(fs))
	for _, f := range fs {
		out = append(out, EvidenceOut{Kind: f.Kind, Commit: shortID(f.Commit), PR: f.PR, Capability: f.Capability, Pursuit: f.Pursuit,
			Change: f.Change, Idea: f.Idea, At: stamp(f.At), Source: f.Source, Seconds: f.Seconds, ByCommit: shortID(f.ByCommit)})
	}
	return out
}

// pursuitEvidence reads the facts for one pursuit, newest first, up to the cap.
func pursuitEvidence(ctx context.Context, st work.Submitter, slug string) []EvidenceOut {
	fs, err := stateevidence.List(ctx, st, stateevidence.Filter{Pursuit: slug, Limit: pursuitEvidenceMax})
	if err != nil {
		log.Printf("pursuit.show %s: evidence: %v", slug, err)
	}
	return evidenceOut(fs)
}

func evidenceMethods(g getter) []api.Method {
	return []api.Method{
		api.Define("evidence.list", "Lists the facts the engine observed in the repo, newest first.",
			[]string{"Facts come from version control and the forge: a change landing, a change reverted, a merged PR's lead time and its CI result. They are observed, never reported, and hold no weight.",
				"It reads what the engine has stored and polls nothing; the engine polls every 5 minutes while a pursuit is active and daily otherwise. The result is [] when there are none.",
				"capability and pursuit narrow the list; limit is 50 by default and at most 500. Commits are shown as their first eight characters."},
			`{"repo":"/home/me/code/shop","pursuit":"try-a-quiet-mode","limit":20}`,
			`[{"kind":"landed","commit":"3f9a2c1d","capability":"cli","pursuit":"try-a-quiet-mode","change":"quiet-flag","at":"2026-11-22T10:00:00Z","source":"vcs"}]`,
			func(ctx context.Context, _ *api.Conn, in EvidenceIn) ([]EvidenceOut, error) {
				st, err := g.open(ctx, in.Repo)
				if err != nil {
					return nil, err
				}
				fs, err := stateevidence.List(ctx, st, stateevidence.Filter{Capability: in.Capability, Pursuit: in.Pursuit, Limit: in.Limit})
				return evidenceOut(fs), refused(err)
			}),
	}
}
