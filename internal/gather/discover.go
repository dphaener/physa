package gather

import (
	"context"
	"errors"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Discover asks for the agent's search for more sources. Its zero value asks
// for none. Off, when set, is the reason not to start one and is reported as
// the why.
type Discover struct {
	Session discovery.Session
	Off     string
}

// discover runs the agent's search over the repo under root, given the
// deterministic finds, and returns the docs it added, what it skipped and
// what it did. It never fails: an agent that is missing, a session that
// fails, and a reply that names nothing leave the finds as they are and say
// why.
func discover(ctx context.Context, root string, repo vcs.Repo, d Discover, found []store.Doc) ([]store.Doc, []Skip, Discovery) {
	var info Discovery
	if d.Off != "" {
		info.Why = d.Off
		return nil, nil, info
	}
	if d.Session.Agent == nil {
		info.Why = "discovery not asked"
		return nil, nil, info
	}
	listed := listedFiles(ctx, repo)

	s := d.Session
	s.Root = root
	var resolveErr error
	agent := s.Agent
	s.Agent = func() (adapter.Adapter, error) {
		a, err := agent()
		resolveErr = err
		return a, err
	}
	res, err := discovery.Run(context.Background(), s)
	info.Model, info.Tokens = res.Model, res.Tokens
	switch {
	case resolveErr != nil:
		info.Why = "no agent: " + resolveErr.Error()
		if !errors.Is(resolveErr, adapter.ErrNotInstalled) {
			info.Why = "no agent: it could not be resolved: " + resolveErr.Error()
		}
		return nil, nil, info
	case err != nil:
		info.Started = true
		info.Why = "the discovery session failed: " + err.Error()
		return nil, nil, info
	}
	info.Started = true
	info.Named = len(res.Sources)
	if len(res.Sources) == 0 {
		info.Why = "the agent named no sources"
		return nil, nil, info
	}
	named := make([]Source, len(res.Sources))
	for i, r := range res.Sources {
		named[i] = Source{Path: r.Path, Kind: r.Kind, Reason: r.Reason}
	}
	added, skipped := Named(root, listed, named, found)
	info.Added = len(added)
	if len(added) == 0 {
		info.Why = "none of the named sources could be read"
	}
	return added, skipped, info
}

// listedFiles is the set of the repo's files the version-control interface
// lists, which gather holds named sources to; nil when the repo cannot list
// them, and the named sources are then checked against the working tree only.
func listedFiles(ctx context.Context, repo vcs.Repo) map[string]bool {
	l, ok := repo.(vcs.Lister)
	if !ok {
		return nil
	}
	paths, err := l.Listed(ctx)
	if err != nil {
		return nil
	}
	listed := make(map[string]bool, len(paths))
	for _, p := range paths {
		listed[p] = true
	}
	return listed
}
