// Command codecheck checks that the core imports nothing above it, that the
// map's side never imports the engine's, and that
// every package stays within its size budget and has a doc comment stating
// its promise. It also checks that no package outside internal/state writes
// the engine's tables (a string literal holding SQL that inserts into,
// updates or deletes from one). It also checks that nothing outside the git adapter
// (internal/vcs/git) runs git: a non-test file calling exec.Command or
// exec.CommandContext with the literal program "git" is reported. It also
// checks that a non-test file on the map's side never makes a fresh context
// with context.Background or context.TODO: the command's context comes down.
// It also checks that no test file assigns a package variable of a non-test
// file, its own package's or another package's of the module through its
// import: a test passes its fake in instead of swapping one.
// It also checks that no non-test file under internal/ reads the process's
// working directory or environment itself, with os.Getwd, os.Getenv,
// os.LookupEnv, os.Environ, os.UserHomeDir, os.UserConfigDir,
// os.UserCacheDir or os.ExpandEnv: the command reads them once and passes
// them down. procAllowlist names each exception and why.
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
	"client":               500,
	"internal/output":      200,
	"internal/configdir":   200,
	"internal/state":       700,
	"internal/repos":       350,
}

// aboveCore lists the path prefixes, relative to the module, of the packages
// that sit above the core: plugins and commands. The core (internal/) must
// never import them.
var aboveCore = []string{"plugins", "cmd"}

// engineSide lists the packages, relative to the module, of the engine: the
// engine process, its API and state, and ideas, pursuits and evidence on top
// of the map. A listed directory covers its subdirectories. Every other
// package under internal/ is the map's side (init, map, check, learn) with
// the foundations both sides use, and must never import one of these, so the
// map works with no engine at all. The engine may import the map's side. A
// new engine package goes here; until it does, a package of it that imports
// the engine is reported.
var engineSide = []string{
	"internal/api",
	"client",
	"internal/apischema",
	"internal/engine",
	"internal/enginecli",
	"internal/enginestart",
	"internal/evidence",
	"internal/evidencecli",
	"internal/ideacli",
	"internal/plan",
	"internal/progress",
	"internal/pursuitcli",
	"internal/repos",
	"internal/signal",
	"internal/state",
	"internal/tokens",
	"internal/workapi",
}

// Rule names reported in violations.
const (
	ruleImports  = "no-plugin-imports"
	ruleEngine   = "map-below-engine"
	ruleSize     = "size-budget"
	ruleDoc      = "package-doc"
	ruleGit      = "git-adapter-only"
	ruleTables   = "engine-tables-writer"
	ruleContext  = "context-from-command"
	ruleTestSwap = "tests-inject-fakes"
	ruleProcess  = "process-from-command"
)

// stateDir is the one package, with its subdirectories, that may write the
// engine's tables (03 §5.1).
const stateDir = "internal/state"

// tablePat names the engine's tables, whole word, optionally quoted.
const tablePat = "[\"`'\\[]?\\b(ideas|pitches|pursuits|plans|plan_revisions|events|intents|timers|changes|session_changes|spends|needs_you|evidence|evidence_polls)\\b[\"`'\\]]?"

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
	swaps, err := testSwaps(root, module, pkgs)
	if err != nil {
		return nil, 0, err
	}
	out = append(out, swaps...)
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
	mapSide := inCore && !onEngineSide(pk.dir)
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
				if rel, ok := strings.CutPrefix(ip, module+"/"); ok && mapSide && onEngineSide(rel) {
					out = append(out, Violation{pk.dir, ruleEngine, fmt.Sprintf("%s imports %s, on the engine's side; the map's side never imports it", name, ip)})
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
		if mapSide {
			for _, pos := range freshContexts(f) {
				out = append(out, Violation{pk.dir, ruleContext, fmt.Sprintf("%s:%d makes a fresh context; take the command's ctx instead", name, fset.Position(pos).Line)})
			}
		}
		if inCore {
			out = append(out, procViolations(pk.dir, name, f, fset, procAllowlist)...)
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

// pkgVars is what a package's non-test files declare: its name and the names
// of its package-level variables.
type pkgVars struct {
	name string
	vars map[string]bool
}

// testSwaps reports every assignment in a test file to a package variable of
// a non-test file: the test's own package (when the test is in that package)
// or a package of the module it imports. A name the enclosing function
// declares shadows the package variable. It reads syntax only, so it is a
// check, not a proof.
func testSwaps(root, module string, pkgs map[string]*pkg) ([]Violation, error) {
	fset := token.NewFileSet()
	parse := func(dir, name string) (*ast.File, error) {
		return parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(dir), name), nil, parser.SkipObjectResolution)
	}
	decls := map[string]pkgVars{}
	for dir, pk := range pkgs {
		pv := pkgVars{vars: map[string]bool{}}
		for _, name := range pk.files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parse(dir, name)
			if err != nil {
				return nil, err
			}
			pv.name = f.Name.Name
			for _, d := range f.Decls {
				if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.VAR {
					for _, spec := range gd.Specs {
						for _, id := range spec.(*ast.ValueSpec).Names {
							if id.Name != "_" {
								pv.vars[id.Name] = true
							}
						}
					}
				}
			}
		}
		decls[dir] = pv
	}
	var out []Violation
	for dir, pk := range pkgs {
		for _, name := range pk.files {
			if !strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parse(dir, name)
			if err != nil {
				return nil, err
			}
			imports := map[string]pkgVars{}
			for _, imp := range f.Imports {
				ip, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return nil, err
				}
				rel, ok := strings.CutPrefix(ip, module+"/")
				if ip == module {
					rel, ok = ".", true
				}
				pv, found := decls[rel]
				if !ok || !found {
					continue
				}
				local := pv.name
				if imp.Name != nil {
					local = imp.Name.Name
				}
				if local != "_" && local != "." {
					imports[local] = pv
				}
			}
			own := pkgVars{}
			if pv := decls[dir]; pv.name == f.Name.Name {
				own = pv
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				declared := declaredNames(fn)
				for _, e := range assignedTargets(fn) {
					if target, ok := swapTarget(e, own, imports, declared); ok {
						out = append(out, Violation{dir, ruleTestSwap, fmt.Sprintf("%s:%d assigns package variable %s; pass the fake in instead", name, fset.Position(e.Pos()).Line, target)})
					}
				}
			}
		}
	}
	return out, nil
}

// swapTarget returns the package variable the assignment target e writes
// through, if any, stripping index, star, paren and field selectors.
func swapTarget(e ast.Expr, own pkgVars, imports map[string]pkgVars, declared map[string]bool) (string, bool) {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name, own.vars[x.Name] && !declared[x.Name]
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && !declared[id.Name] {
				if pv, ok := imports[id.Name]; ok && pv.vars[x.Sel.Name] {
					return id.Name + "." + x.Sel.Name, true
				}
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return "", false
		}
	}
}

// assignedTargets returns the targets of every `=`, op-assign, inc/dec and
// range-with-`=` in fn, closures included; `:=` declares and is left out.
func assignedTargets(fn *ast.FuncDecl) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				out = append(out, s.Lhs...)
			}
		case *ast.IncDecStmt:
			out = append(out, s.X)
		case *ast.RangeStmt:
			if s.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{s.Key, s.Value} {
					if e != nil {
						out = append(out, e)
					}
				}
			}
		}
		return true
	})
	return out
}

// declaredNames returns every name fn declares anywhere in it, closures
// included: receiver, parameters, results, `:=`, var, const, type and range
// names. Such a name shadows a package variable of the same name.
func declaredNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	fields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, id := range f.Names {
				names[id.Name] = true
			}
		}
	}
	fields(fn.Recv)
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncType:
			fields(x.Params)
			fields(x.Results)
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, e := range x.Lhs {
					if id, ok := e.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				for _, e := range []ast.Expr{x.Key, x.Value} {
					if id, ok := e.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				names[id.Name] = true
			}
		case *ast.TypeSpec:
			names[x.Name.Name] = true
		}
		return true
	})
	return names
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

// freshContexts returns the positions of calls to the context package's
// Background or TODO, honouring an import alias of "context".
func freshContexts(f *ast.File) []token.Pos {
	names := map[string]bool{}
	for _, imp := range f.Imports {
		if ip, err := strconv.Unquote(imp.Path.Value); err != nil || ip != "context" {
			continue
		}
		name := "context"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name != "_" && name != "." {
			names[name] = true
		}
	}
	if len(names) == 0 {
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
		if id, ok := sel.X.(*ast.Ident); ok && names[id.Name] && (sel.Sel.Name == "Background" || sel.Sel.Name == "TODO") {
			out = append(out, call.Pos())
		}
		return true
	})
	return out
}

// procAllowlist maps "<path from the module root> os.<Name>" to the reason
// that call is allowed. A listed call is not reported; another call in the
// same file still is.
var procAllowlist = map[string]string{}

// procFuncs are the functions of os that read the process's working
// directory or environment.
var procFuncs = map[string]bool{
	"Getwd": true, "Getenv": true, "LookupEnv": true, "Environ": true,
	"UserHomeDir": true, "UserConfigDir": true, "UserCacheDir": true, "ExpandEnv": true,
}

// procRead is one use of a function of os in procFuncs, called or handed on
// as a value.
type procRead struct {
	pos  token.Pos
	name string
}

// procReads returns the uses in f of the functions of procFuncs, called or
// passed as a value (os.Getenv handed to a function), honouring an import
// alias of "os". A method of the same name on another value is
// not one.
func procReads(f *ast.File) []procRead {
	names := map[string]bool{}
	for _, imp := range f.Imports {
		if ip, err := strconv.Unquote(imp.Path.Value); err != nil || ip != "os" {
			continue
		}
		name := "os"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name != "_" && name != "." {
			names[name] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	var out []procRead
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && names[id.Name] && procFuncs[sel.Sel.Name] {
			out = append(out, procRead{sel.Pos(), sel.Sel.Name})
		}
		return true
	})
	return out
}

// procViolations reports the reads of f, the file name in the package dir,
// that allow does not list.
func procViolations(dir, name string, f *ast.File, fset *token.FileSet, allow map[string]string) []Violation {
	var out []Violation
	for _, r := range procReads(f) {
		if _, ok := allow[dir+"/"+name+" os."+r.name]; ok {
			continue
		}
		out = append(out, Violation{dir, ruleProcess, fmt.Sprintf("%s:%d calls os.%s; take it from the command's process state instead", name, fset.Position(r.pos).Line, r.name)})
	}
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

// onEngineSide reports whether dir, relative to the module, is at or under
// a package of engineSide.
func onEngineSide(dir string) bool {
	for _, e := range engineSide {
		if dir == e || strings.HasPrefix(dir, e+"/") {
			return true
		}
	}
	return false
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
