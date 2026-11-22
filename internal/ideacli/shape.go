package ideacli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/workapi"
)

// shape sends a pitch for an idea: read with --from from a file or stdin,
// else written by a person in their editor; off a terminal --from is the
// only way.
func (e *Env) shape(args []string, stdout, stderr io.Writer) int {
	f, ok := Parse(args, "from", "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	if f.From == "" && !e.Interactive() {
		return Failed(stderr, "idea shape", &UsageError{"off a terminal, give the pitch with --from <file>, or --from - to read it from stdin"})
	}
	repo, err := e.Repo()
	if err != nil {
		return Failed(stderr, "idea shape", err)
	}
	slug, err := e.chooseIdea(repo, f.Args)
	if err != nil {
		return Failed(stderr, "idea shape", err)
	}
	who := e.Who(repo, stderr)
	if who == "" {
		return 2
	}
	send := func(md string) (workapi.IdeaOut, error) {
		var out workapi.IdeaOut
		err := e.Call("idea.shape", workapi.ShapeIn{Repo: repo, Slug: slug, Markdown: md, Person: who}, &out)
		return out, err
	}
	var out workapi.IdeaOut
	if f.From != "" {
		md, err := e.readFrom(f.From)
		if err == nil {
			out, err = send(md)
		}
		if err != nil {
			return Failed(stderr, "idea shape", err)
		}
	} else if out, ok = e.shapeInEditor(repo, slug, send, stderr); !ok {
		return 2
	}
	if f.JSON {
		return Written(stderr, "idea shape", output.JSON(stdout, out))
	}
	v := e.View(stdout)
	v.Title("Shaped idea " + out.Slug)
	appetite := ""
	if out.Pitch != nil {
		appetite = pitch.Tokens(out.Pitch.Appetite)
	}
	v.Fields([2]string{"appetite", appetite}, [2]string{"facts", Facts(out)})
	return 0
}

// readFrom reads the pitch from the file at path, or from stdin for "-".
func (e *Env) readFrom(path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(e.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	return string(b), err
}

// shapeInEditor writes the idea's template, prefilled from its latest
// pitch, to a temporary file and opens it in the person's editor until the
// pitch sent from it is taken or the person stops. The file is removed once
// taken, and kept, with its path printed, otherwise.
func (e *Env) shapeInEditor(repo, slug string, send func(string) (workapi.IdeaOut, error), stderr io.Writer) (workapi.IdeaOut, bool) {
	var idea workapi.IdeaOut
	if err := e.Call("idea.show", workapi.SlugIn{Repo: repo, Slug: slug}, &idea); err != nil {
		Failed(stderr, "idea shape", err)
		return idea, false
	}
	var prior *pitch.Pitch
	if p := idea.Pitch; p != nil {
		prior = &pitch.Pitch{Problem: p.Problem, Appetite: p.Appetite, Solution: p.Solution, Scope: p.Scope,
			Signal: p.Signal, RabbitHoles: p.RabbitHoles, NoGos: p.NoGos}
	}
	path, err := writeTemp(pitch.Template(idea.Text, prior))
	if err != nil {
		Failed(stderr, "idea shape", err)
		return idea, false
	}
	kept := func(err error) (workapi.IdeaOut, bool) {
		if err != nil {
			Failed(stderr, "idea shape", err)
		}
		fmt.Fprintf(stderr, "physa idea shape: the pitch is kept at %s\n", path)
		return idea, false
	}
	for {
		if err := e.edit(path); err != nil {
			return kept(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return kept(err)
		}
		out, err := send(string(b))
		if err == nil {
			os.Remove(path)
			return out, true
		}
		if !BadRequest(err) {
			return kept(err)
		}
		Failed(stderr, "idea shape", err)
		again, cerr := e.Confirm("Edit the pitch again?")
		if cerr != nil || !again {
			return kept(nil)
		}
	}
}

// writeTemp writes md to a new temporary file and returns its path.
func writeTemp(md string) (string, error) {
	f, err := os.CreateTemp("", "physa-pitch-*.md")
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(md)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// edit opens path in $VISUAL, else $EDITOR, else vi, on the terminal. The
// variable may carry arguments, split on spaces.
func (e *Env) edit(path string) error {
	editor := strings.Fields(e.Getenv("VISUAL"))
	if len(editor) == 0 {
		editor = strings.Fields(e.Getenv("EDITOR"))
	}
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	cmd := exec.Command(editor[0], append(editor[1:], path)...)
	if e.Stdio[0] != nil {
		cmd.Stdin = e.Stdio[0]
	}
	if e.Stdio[1] != nil {
		cmd.Stdout = e.Stdio[1]
	}
	if e.Stdio[2] != nil {
		cmd.Stderr = e.Stdio[2]
	}
	if err := cmd.Run(); err != nil {
		return errors.New("the editor " + editor[0] + " failed: " + err.Error())
	}
	return nil
}
