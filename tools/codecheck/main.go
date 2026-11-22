// Command codecheck checks that the core imports nothing above it and that
// every package stays within its size budget and has a doc comment stating
// its promise. It also checks that no package outside internal/state writes
// the engine's tables (a string literal holding SQL that inserts into,
// updates or deletes from one). It also checks that nothing outside the git adapter
// (internal/vcs/git) runs git: a non-test file calling exec.Command or
// exec.CommandContext with the literal program "git" is reported.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sizeBudget is the most non-blank, non-test lines a package may hold. Going
// over means cutting scope, not raising the budget.
const sizeBudget = 800

// packageBudgets gives the engine's packages budgets lower than sizeBudget
// (03 §13: "Its packages get size budgets from day one"). A listed package is
// checked against its entry, never against more than sizeBudget. An entry for
// a directory that does not exist yet is harmless.
var packageBudgets = map[string]int{
	"internal/engine":      400,
	"internal/api":         400,
	"internal/apischema":   400,
	"internal/enginestart": 400,
	"internal/enginecli":   400,
	"internal/apiclient":   300,
	"internal/output":      200,
	"internal/configdir":   200,
	"internal/state":       700,
	"internal/repos":       350,
}

// aboveCore lists the path prefixes, relative to the module, of the packages
// that sit above the core: plugins and commands. The core (internal/) must
// never import them.
var aboveCore = []string{"plugins", "cmd"}

// Rule names reported in violations.
const (
	ruleImports = "no-plugin-imports"
	ruleSize    = "size-budget"
	ruleDoc     = "package-doc"
	ruleGit     = "git-adapter-only"
	ruleTables  = "engine-tables-writer"
)

// stateDir is the one package, with its subdirectories, that may write the
// engine's tables (03 §5.1).
const stateDir = "internal/state"

// tablePat names the engine's tables, whole word, optionally quoted.
const tablePat = "[\"`'\\[]?\\b(ideas|pitches|bets|plans|plan_revisions|events|intents|timers)\\b[\"`'\\]]?"

// writePats match SQL in its write shape; group 1 of each is the table.
var writePats = func() []*regexp.Regexp {
	tail := `\s*(?:\(|VALUES\b|SELECT\b|DEFAULT\b)`
	shapes := []string{
		`\bINSERT(?:\s+OR\s+\w+)?\s+INTO\s+` + tablePat + tail,
		`\bREPLACE\s+INTO\s+` + tablePat + tail,
		`\bUPDATE(?:\s+OR\s+\w+)?\s+` + tablePat + `\s+SET\b`,
		`\bDELETE\s+FROM\s+` + tablePat + `\s*(?:WHERE\b|;|$)`,
	}
	out := make([]*regexp.Regexp, len(shapes))
	for i, sh := range shapes {
		out[i] = regexp.MustCompile(`(?is)` + sh)
	}
	return out
}()

// gitAdapterDir is the one package, relative to the module, that may run git.
const gitAdapterDir = "internal/vcs/git"

// Violation is one broken rule in one package.
type Violation struct {
	Dir     string // package directory, relative to the root, slash-separated
	Rule    string
	Message string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	root := "."
	switch len(args) {
	case 0:
	case 1:
		root = args[0]
	default:
		fmt.Fprintln(stderr, "usage: codecheck [module-root]")
		return 2
	}
	violations, n, err := scan(root, packageBudgets)
	if err != nil {
		fmt.Fprintf(stderr, "codecheck: %v\n", err)
		return 2
	}
	if len(violations) > 0 {
		lines := make([]string, len(violations))
		for i, v := range violations {
			lines[i] = fmt.Sprintf("%s: %s: %s", v.Dir, v.Rule, v.Message)
		}
		sort.Strings(lines)
		for _, l := range lines {
			fmt.Fprintln(stderr, l)
		}
		return 1
	}
	fmt.Fprintf(stdout, "codecheck: ok (%d packages)\n", n)
	return 0
}

func check(root string) ([]Violation, error) {
	v, _, err := scan(root, packageBudgets)
	return v, err
}

// pkg is one directory of Go files.
type pkg struct {
	dir   string
	files []string // file names within dir
}

// scan checks every package under root. budgets maps a package directory,
// relative to root, to its budget; packages not in it get sizeBudget.
func scan(root string, budgets map[string]int) ([]Violation, int, error) {
	module, err := modulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, 0, err
	}
	pkgs := map[string]*pkg{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if pkgs[rel] == nil {
			pkgs[rel] = &pkg{dir: rel}
		}
		pkgs[rel].files = append(pkgs[rel].files, d.Name())
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	var out []Violation
	for _, pk := range pkgs {
		vs, err := checkPackage(root, module, pk, budgets)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, vs...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Dir != b.Dir {
			return a.Dir < b.Dir
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
	return out, len(pkgs), nil
}

func checkPackage(root, module string, pk *pkg, budgets map[string]int) ([]Violation, error) {
	var out []Violation
	fset := token.NewFileSet()
	inCore := pk.dir == "internal" || strings.HasPrefix(pk.dir, "internal/")
	lines := 0
	hasDoc, docOK := false, false
	pkgName := ""
	var wrongDoc string
	sort.Strings(pk.files)
	for _, name := range pk.files {
		full := filepath.Join(root, filepath.FromSlash(pk.dir), name)
		f, err := parser.ParseFile(fset, full, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		isTest := strings.HasSuffix(name, "_test.go")
		if inCore {
			for _, imp := range f.Imports {
				ip, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return nil, err
				}
				if forbidden(module, ip) {
					out = append(out, Violation{pk.dir, ruleImports, fmt.Sprintf("%s imports %s", name, ip)})
				}
			}
		}
		if isTest {
			continue
		}
		if pk.dir != gitAdapterDir {
			for _, pos := range gitCalls(f) {
				out = append(out, Violation{pk.dir, ruleGit, fmt.Sprintf("%s:%d runs git; only %s may", name, fset.Position(pos).Line, gitAdapterDir)})
			}
		}
		if pk.dir != stateDir && !strings.HasPrefix(pk.dir, stateDir+"/") {
			for _, w := range tableWrites(f, fset) {
				out = append(out, Violation{pk.dir, ruleTables, fmt.Sprintf("%s:%d: writes the engine's table %s; only %s writes it (03 §5.1)", name, w.line, w.table, stateDir)})
			}
		}
		n, err := countLines(full)
		if err != nil {
			return nil, err
		}
		lines += n
		if pkgName == "" {
			pkgName = f.Name.Name
		}
		if f.Doc != nil && strings.TrimSpace(f.Doc.Text()) != "" {
			hasDoc = true
			if f.Name.Name == "main" || strings.HasPrefix(f.Doc.Text(), "Package "+f.Name.Name+" ") {
				docOK = true
			} else if wrongDoc == "" {
				wrongDoc = name
			}
		}
	}
	budget := sizeBudget
	if b, ok := budgets[pk.dir]; ok && b < sizeBudget {
		budget = b
	}
	if lines > budget {
		out = append(out, Violation{pk.dir, ruleSize, fmt.Sprintf("%d non-blank lines, budget is %d", lines, budget)})
	}
	if pkgName != "" && !docOK {
		msg := "no doc comment on the package clause"
		if hasDoc {
			msg = fmt.Sprintf("doc comment in %s must start with %q", wrongDoc, "Package "+pkgName+" ")
		}
		out = append(out, Violation{pk.dir, ruleDoc, msg})
	}
	return out, nil
}

// gitCalls returns the positions of calls to os/exec's Command or
// CommandContext whose program argument is the string literal "git".
func gitCalls(f *ast.File) []token.Pos {
	name := ""
	for _, imp := range f.Imports {
		if ip, err := strconv.Unquote(imp.Path.Value); err != nil || ip != "os/exec" {
			continue
		}
		name = "exec"
		if imp.Name != nil {
			name = imp.Name.Name
		}
	}
	if name == "" || name == "_" || name == "." {
		return nil
	}
	var out []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != name {
			return true
		}
		prog := 0
		switch sel.Sel.Name {
		case "Command":
		case "CommandContext":
			prog = 1
		default:
			return true
		}
		if prog >= len(call.Args) {
			return true
		}
		if lit, ok := call.Args[prog].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil && v == "git" {
				out = append(out, call.Pos())
			}
		}
		return true
	})
	return out
}

// tableWrite is one string literal that writes one of the engine's tables.
type tableWrite struct {
	line  int
	table string
}

// tableWrites returns the single string literals of f that hold SQL writing
// one of the engine's tables. Literals built by concatenation or Sprintf are
// out of reach: this is a check, not a proof.
func tableWrites(f *ast.File, fset *token.FileSet) []tableWrite {
	var out []tableWrite
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		seen := map[string]bool{}
		for _, re := range writePats {
			for _, m := range re.FindAllStringSubmatch(v, -1) {
				if t := strings.ToLower(m[1]); !seen[t] {
					seen[t] = true
					out = append(out, tableWrite{fset.Position(lit.Pos()).Line, t})
				}
			}
		}
		return true
	})
	return out
}

// forbidden reports whether import path ip is at or under a package above the core.
func forbidden(module, ip string) bool {
	for _, p := range aboveCore {
		prefix := path.Join(module, p)
		if ip == prefix || strings.HasPrefix(ip, prefix+"/") {
			return true
		}
	}
	return false
}

func countLines(file string) (int, error) {
	fh, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	n := 0
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			n++
		}
	}
	return n, sc.Err()
}

func modulePath(gomod string) (string, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", fmt.Errorf("%s: no module line", gomod)
}
