// Package ideacli is the idea commands and what the pursuit commands share:
// talking to the engine, the one interactive test, choosing a slug, opening
// an editor, and styles.
package ideacli

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/tokens"
	"github.com/dphaener/physa/internal/workapi"
)

// Command returns the idea group, naming people through the forge newForge
// builds for a repo.
func Command(newForge func(dir string) forge.Forge) *cli.Command {
	return New(newForge).command()
}

func (e *Env) command() *cli.Command {
	return &cli.Command{
		Name: "idea",
		Summary: "Add, list and show ideas: a bare idea lists them. An idea belongs to the person who adds it; " +
			"its facts say where it stands. A slug left out is chosen from a list on a terminal.",
		Usage: []string{
			"physa idea [--json]",
			`physa idea add "<text>" [--slug <slug>] [--json]`,
			"physa idea list [--json]",
			"physa idea show [<slug>] [--json]",
		},
		Run: e.list,
		Subs: []*cli.Command{
			leaf("add", `physa idea add "<text>" [--slug <slug>] [--json]`,
				"Add an idea and print its slug, made from the text unless --slug chooses it.", e.add),
			leaf("list", "physa idea list [--json]",
				"List the ideas, newest first, with who added them and their facts.", e.list),
			leaf("show", "physa idea show [<slug>] [--json]",
				"Show an idea: its text, who added it, and its pursuits.", e.show),
		},
	}
}

func leaf(name, usage, summary string, run func(args []string, stdout, stderr io.Writer) int) *cli.Command {
	return &cli.Command{Name: name, Summary: summary, Usage: []string{usage}, Run: run}
}

func (e *Env) add(args []string, stdout, stderr io.Writer) int {
	f, ok := Parse(args, "slug", "json")
	if !ok || len(f.Args) != 1 {
		return cli.BadUsage
	}
	repo, err := e.Repo()
	if err != nil {
		return Failed(stderr, "idea add", err)
	}
	who := e.Who(repo, stderr)
	if who == "" {
		return 2
	}
	var out workapi.IdeaOut
	if err := e.Call("idea.add", workapi.AddIn{Repo: repo, Text: f.Args[0], Slug: f.Slug, Person: who}, &out); err != nil {
		return Failed(stderr, "idea add", err)
	}
	if f.JSON {
		return Written(stderr, "idea add", output.JSON(stdout, out))
	}
	e.View(stdout).Title("Added idea " + out.Slug)
	return 0
}

// Ideas lists the repo's ideas, newest first.
func (e *Env) Ideas(repo string) ([]workapi.IdeaOut, error) {
	var out []workapi.IdeaOut
	err := e.Call("idea.list", workapi.RepoIn{Repo: repo}, &out)
	return out, err
}

func (e *Env) list(args []string, stdout, stderr io.Writer) int {
	f, ok := Parse(args, "json")
	if !ok || len(f.Args) > 0 {
		return cli.BadUsage
	}
	repo, err := e.Repo()
	if err != nil {
		return Failed(stderr, "idea list", err)
	}
	ideas, err := e.Ideas(repo)
	if err != nil {
		return Failed(stderr, "idea list", err)
	}
	if f.JSON {
		return Written(stderr, "idea list", output.JSON(stdout, ideas))
	}
	v := e.View(stdout)
	if len(ideas) == 0 {
		v.Line(`No ideas yet. Add one with physa idea add "<text>".`)
		return 0
	}
	rows := make([][]string, len(ideas))
	for i, o := range ideas {
		rows[i] = []string{o.Slug, o.Text, o.Person, Facts(o)}
	}
	v.Table([]string{"Slug", "Idea", "Person", "Facts"}, rows)
	return 0
}

// Facts says where an idea stands: its latest pursuit's state, or that it
// was never pursued.
func Facts(o workapi.IdeaOut) string {
	switch {
	case o.Pursuit == "":
		return "not pursued"
	case o.Pursuit == o.Slug:
		return "pursuit " + o.PursuitState
	}
	return "pursuit " + o.Pursuit + " " + o.PursuitState
}

// Appetite writes a pursuit's appetite, or "none" when it has none.
func Appetite(n int64) string {
	if n <= 0 {
		return "none"
	}
	return tokens.Format(n)
}

// IdeaChoices makes the choices for ChooseSlug from ideas.
func IdeaChoices(ideas []workapi.IdeaOut) []Choice {
	cs := make([]Choice, len(ideas))
	for i, o := range ideas {
		cs[i] = Choice{Slug: o.Slug, Text: o.Text, Detail: Facts(o)}
	}
	return cs
}

// chooseIdea returns the slug given in args, or the one a person chooses
// from the repo's ideas.
func (e *Env) chooseIdea(repo string, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	ideas, err := e.Ideas(repo)
	if err != nil {
		return "", err
	}
	return e.ChooseSlug("idea", "", IdeaChoices(ideas))
}

func (e *Env) show(args []string, stdout, stderr io.Writer) int {
	f, ok := Parse(args, "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	repo, err := e.Repo()
	if err != nil {
		return Failed(stderr, "idea show", err)
	}
	slug, err := e.chooseIdea(repo, f.Args)
	if err != nil {
		return Failed(stderr, "idea show", err)
	}
	var out workapi.IdeaOut
	if err := e.Call("idea.show", workapi.SlugIn{Repo: repo, Slug: slug}, &out); err != nil {
		return Failed(stderr, "idea show", err)
	}
	if f.JSON {
		return Written(stderr, "idea show", output.JSON(stdout, out))
	}
	e.showIdea(e.View(stdout), out)
	return 0
}

func (e *Env) showIdea(v *View, o workapi.IdeaOut) {
	v.Title(o.Text)
	pursuits := "none"
	if len(o.Pursuits) > 0 {
		ps := make([]string, len(o.Pursuits))
		for i, p := range o.Pursuits {
			ps[i] = fmt.Sprintf("%s (%s, appetite %s, spent %s)", p.Slug, p.State, Appetite(p.Appetite), tokens.Format(p.Spent))
		}
		pursuits = strings.Join(ps, ", ")
	}
	v.Fields(
		[2]string{"slug", o.Slug},
		[2]string{"person", o.Person},
		[2]string{"added", When(o.CreatedAt)},
		[2]string{"facts", Facts(o)},
		[2]string{"pursuits", pursuits},
	)
}
