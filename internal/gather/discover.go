package gather

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// maxTree is the most paths of the repo's tree that discovery is shown.
const maxTree = 4000

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
// fails or is stopped, and a reply that names nothing leave the finds as they
// are and say why.
func discover(root string, repo vcs.Repo, d Discover, found []store.Doc) ([]store.Doc, []Skip, Discovery) {
	info := Discovery{Cap: d.Session.Appetite}
	if d.Off != "" {
		info.Why = d.Off
		return nil, nil, info
	}
	if d.Session.Agent == nil {
		info.Why = "discovery not asked"
		return nil, nil, info
	}
	if why := discovery.TooSmall(d.Session.Appetite); why != "" {
		info.Why = why
		return nil, nil, info
	}
	tree, listed := fileTree(root, repo)
	in := discovery.Input{Found: found}
	if len(tree) > maxTree {
		in.Truncated = len(tree) - maxTree
		tree = tree[:maxTree]
	}
	in.Tree = tree

	s := d.Session
	s.Root = root
	var resolveErr error
	agent := s.Agent
	s.Agent = func() (adapter.Adapter, error) {
		a, err := agent()
		resolveErr = err
		return a, err
	}
	res, err := discovery.Run(context.Background(), s, in)
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
	if res.Stopped {
		info.Why = "stopped at the token cap"
		return nil, nil, info
	}
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

// fileTree lists the repo's files as slash paths, sorted: through the repo
// when it can list them, and otherwise by walking root, skipping .git and
// the ignored folders. listed is the set of the repo's listed files, or nil
// when the tree came from a walk.
func fileTree(root string, repo vcs.Repo) (tree []string, listed map[string]bool) {
	if l, ok := repo.(vcs.Lister); ok {
		if paths, err := l.Listed(); err == nil {
			listed = make(map[string]bool, len(paths))
			for _, p := range paths {
				listed[p] = true
			}
			return paths, listed
		}
	}
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() && (e.Name() == ".git" || ignoredDirs[e.Name()]) {
			return filepath.SkipDir
		}
		if rel, err := filepath.Rel(root, p); err == nil && !e.IsDir() {
			tree = append(tree, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(tree)
	return tree, nil
}
