package enginecli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/output"
)

func (g group) apiCommand() *cli.Command {
	return &cli.Command{
		Name: "api",
		Summary: "Call the engine's API by name, for debugging: a bare api lists its methods. " +
			"Each command starts the engine when it is not running.",
		Usage: []string{
			"physa api [--json]",
			"physa api list [--json]",
			"physa api describe <method> [--json]",
			"physa api call <method> [<json>|-]",
		},
		Run: g.apiList,
		Subs: []*cli.Command{
			leaf("list", "physa api list [--json]",
				"List the API's methods with their summaries.", g.apiList),
			leaf("describe", "physa api describe <method> [--json]",
				"Describe one method: its summary, the schemas of its input and output, examples and notes.", g.apiDescribe),
			leaf("call", "physa api call <method> [<json>|-]",
				"Call a method with JSON params (- reads them from stdin; none means {}) and print its result as JSON.", g.apiCall),
		},
	}
}

func (g group) apiList(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args, "json")
	if !ok || len(f.args) > 0 {
		return cli.BadUsage
	}
	var methods []api.MethodSummary
	if err := g.call(0, "api.list", nil, &methods); err != nil {
		return failed(stderr, "api list", err)
	}
	if f.json {
		return written(stderr, "api list", output.JSON(stdout, methods))
	}
	rows := make([][]string, 0, len(methods))
	for _, m := range methods {
		rows = append(rows, []string{m.Name, m.Summary})
	}
	output.For(stdout).Table([]string{"Method", "Summary"}, rows)
	return 0
}

// apiDescribe prints a method's description: its summary, name, schemas,
// examples and notes, in that order.
func (g group) apiDescribe(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args, "json")
	if !ok || len(f.args) != 1 {
		return cli.BadUsage
	}
	var d api.Description
	if err := g.call(0, "api.describe", api.DescribeInput{Name: f.args[0]}, &d); err != nil {
		return failed(stderr, "api describe", err)
	}
	if f.json {
		return written(stderr, "api describe", output.JSON(stdout, d))
	}
	// A schema always marshals: it holds only strings, bools and schemas.
	in, _ := json.Marshal(d.Input)
	out, _ := json.Marshal(d.Output)
	p := output.For(stdout)
	p.Heading(d.Summary)
	p.Line("")
	p.Line("Method: " + d.Name)
	for _, sec := range [][2]string{
		{"Input schema:", string(in)},
		{"Output schema:", string(out)},
		{"Request example:", d.RequestExample},
		{"Response example:", d.ResponseExample},
	} {
		p.Line("\n" + sec[0])
		var b bytes.Buffer
		if json.Indent(&b, []byte(sec[1]), "  ", "  ") != nil {
			b.Reset()
			b.WriteString(sec[1]) // not JSON: kept as it is
		}
		p.Line("  " + b.String())
	}
	if len(d.Notes) > 0 {
		p.Line("\nNotes:")
		for _, n := range d.Notes {
			p.Line("- " + n)
		}
	}
	return 0
}

// apiCall calls a method with the params given, read from stdin for "-",
// and prints the result as JSON.
func (g group) apiCall(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args)
	if !ok || len(f.args) < 1 || len(f.args) > 2 {
		return cli.BadUsage
	}
	params := []byte("{}")
	if len(f.args) == 2 && f.args[1] == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return failed(stderr, "api call", err)
		}
		params = b
	} else if len(f.args) == 2 {
		params = []byte(f.args[1])
	}
	if !json.Valid(params) {
		return failed(stderr, "api call", errors.New("the params are not valid JSON"))
	}
	var result json.RawMessage
	if err := g.call(0, f.args[0], json.RawMessage(params), &result); err != nil {
		return failed(stderr, "api call", err)
	}
	return written(stderr, "api call", output.JSON(stdout, result))
}
