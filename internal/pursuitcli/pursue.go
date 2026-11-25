package pursuitcli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/tokens"
)

const usagePursue = "physa pursue [<idea slug>] [--appetite <tokens>] [--signal-from <file>|-] [--json]"

// start starts a pursuit of an idea: the signal read with --signal-from from
// a file or stdin, else written by a person in their editor; off a terminal
// --signal-from is the only way.
func (p pursuits) start(args []string, appetite, signalFrom string, asJSON bool, stdout, stderr io.Writer) int {
	if p.refuse("pursuit.start", "pursue", stderr) {
		return 2
	}
	if len(args) > 1 {
		return command.BadUsage
	}
	if signalFrom == "" && !p.Interactive() {
		return ideacli.Failed(stderr, "pursue", &ideacli.UsageError{Msg: "off a terminal, give the signal with --signal-from <file>, or --signal-from - to read it from stdin"})
	}
	if appetite != "" {
		if _, err := tokens.Parse(appetite); err != nil {
			return ideacli.Failed(stderr, "pursue", err)
		}
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursue", err)
	}
	who := p.Who(repo, stderr)
	if who == "" {
		return 2
	}
	ideas, err := p.Ideas(repo)
	if err != nil {
		return ideacli.Failed(stderr, "pursue", err)
	}
	var open []client.IdeaOut
	text := map[string]string{}
	for _, o := range ideas {
		text[o.Slug] = o.Text
		if o.PursuitState != "active" {
			open = append(open, o)
		}
	}
	slug, err := p.ChooseSlug("idea", firstArg(args), ideacli.IdeaChoices(open))
	if err != nil {
		return ideacli.Failed(stderr, "pursue", err)
	}
	var sig signal.Signal
	var ok bool
	if signalFrom != "" {
		md, err := p.ReadFrom(signalFrom)
		if err == nil {
			sig, err = signal.Parse(md, time.Now())
		}
		if err != nil {
			return ideacli.Failed(stderr, "pursue", err)
		}
	} else if sig, ok = p.signalInEditor(text[slug], stderr); !ok {
		return 2
	}
	var out client.PursuitOut
	in := client.StartIn{Repo: repo, Slug: slug, Appetite: appetite, Person: who, Signal: client.SignalOut{
		Claim: sig.Claim, For: sig.For, Against: sig.Against, Window: sig.Window, Resolver: sig.Resolver}}
	if err := p.Call("pursuit.start", in, &out); err != nil {
		return ideacli.Failed(stderr, "pursue", err)
	}
	if asJSON {
		return ideacli.Written(stderr, "pursue", output.JSON(stdout, out))
	}
	p.View(stdout).Title(fmt.Sprintf("Pursuing %q as %s, appetite %s, read on %s", out.IdeaText, out.Slug, ideacli.Appetite(out.Appetite), sig.Window))
	return 0
}

// signalInEditor writes the signal's template to a temporary file and opens
// it in the person's editor until what they write parses or they stop. The
// file is removed once it parses, and kept, with its path printed,
// otherwise.
func (p pursuits) signalInEditor(ideaText string, stderr io.Writer) (signal.Signal, bool) {
	path, err := ideacli.WriteTemp("physa-signal-*.md", signal.Template(ideaText))
	if err != nil {
		ideacli.Failed(stderr, "pursue", err)
		return signal.Signal{}, false
	}
	kept := func(err error) (signal.Signal, bool) {
		if err != nil {
			ideacli.Failed(stderr, "pursue", err)
		}
		fmt.Fprintf(stderr, "physa pursue: the signal is kept at %s\n", path)
		return signal.Signal{}, false
	}
	for {
		if err := p.Edit(path); err != nil {
			return kept(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return kept(err)
		}
		sig, err := signal.Parse(string(b), time.Now())
		if err == nil {
			os.Remove(path)
			return sig, true
		}
		ideacli.Failed(stderr, "pursue", err)
		again, cerr := p.Confirm("Edit the signal again?")
		if cerr != nil || !again {
			return kept(nil)
		}
	}
}
