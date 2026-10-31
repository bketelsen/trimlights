package service

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// declContext names the top-level declaration that contains an identifier.
type declContext struct {
	file string // module-relative, slash-separated path
	recv string // receiver type name for a method
	name string // function, method or type name; empty for var/const/import
}

// dormantEntry is one guarded identifier and the only declarations allowed to
// name it in non-test code. Calendar execution and ownership handoff stay dormant
// (docs/specs/calendar-runtime.md, docs/specs/calendar-handoff.md): no startup
// path, route, flag or CLI command may construct the runner or the handoff
// coordinator, or claim ownership. Tests may use them freely.
type dormantEntry struct {
	file    string
	allowed func(declContext) bool
}

// dormantEntries is the allowed list. Each entry permits only the definition
// itself and, for the dormant types, their own methods and constructor:
//
//   - newCalendarRunner: its declaration in internal/service/calendar_runner.go.
//   - calendarRunner: its type declaration, newCalendarRunner and its methods.
//   - newCalendarHandoff: its declaration in internal/service/calendar_handoff.go.
//   - calendarHandoff: its type declaration, newCalendarHandoff and its methods.
//   - ClaimCalendar: the (*Store).ClaimCalendar declaration in
//     internal/storage/calendar_runtime.go.
//
// Any other reference (a call, a method value, a composite literal, a helper in
// the same file, a route handler or a shadowing declaration) is a violation.
// Changing this list is an activation decision: record it in an ADR first.
var dormantEntries = map[string]dormantEntry{
	"newCalendarRunner": {"internal/service/calendar_runner.go", func(c declContext) bool {
		return c.recv == "" && c.name == "newCalendarRunner"
	}},
	"calendarRunner": {"internal/service/calendar_runner.go", func(c declContext) bool {
		return c.recv == "calendarRunner" || (c.recv == "" && (c.name == "calendarRunner" || c.name == "newCalendarRunner"))
	}},
	"newCalendarHandoff": {"internal/service/calendar_handoff.go", func(c declContext) bool {
		return c.recv == "" && c.name == "newCalendarHandoff"
	}},
	"calendarHandoff": {"internal/service/calendar_handoff.go", func(c declContext) bool {
		return c.recv == "calendarHandoff" || (c.recv == "" && (c.name == "calendarHandoff" || c.name == "newCalendarHandoff"))
	}},
	"ClaimCalendar": {"internal/storage/calendar_runtime.go", func(c declContext) bool {
		return c.recv == "Store" && c.name == "ClaimCalendar"
	}},
}

func receiverName(f *ast.FuncDecl) string {
	if f.Recv == nil || len(f.Recv.List) == 0 {
		return ""
	}
	t := f.Recv.List[0].Type
	for {
		switch v := t.(type) {
		case *ast.StarExpr:
			t = v.X
		case *ast.IndexExpr:
			t = v.X
		case *ast.IndexListExpr:
			t = v.X
		case *ast.Ident:
			return v.Name
		default:
			return ""
		}
	}
}

// dormancyScan reports references outside the allowed list and the guarded
// identifiers it saw in an allowed declaration.
func dormancyScan(files map[string]*ast.File, fset *token.FileSet) (violations []string, defined map[string]bool) {
	defined = map[string]bool{}
	for path, file := range files {
		for _, decl := range file.Decls {
			ctx := declContext{file: path}
			switch d := decl.(type) {
			case *ast.FuncDecl:
				ctx.recv, ctx.name = receiverName(d), d.Name.Name
			case *ast.GenDecl:
				if d.Tok == token.TYPE && len(d.Specs) == 1 {
					ctx.name = d.Specs[0].(*ast.TypeSpec).Name.Name
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				entry, guarded := dormantEntries[id.Name]
				if !guarded {
					return true
				}
				if path == entry.file && entry.allowed(ctx) {
					defined[id.Name] = true
					return true
				}
				pos := fset.Position(id.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, id.Name))
				return true
			})
		}
	}
	sort.Strings(violations)
	return violations, defined
}

func TestDormantCalendarEntryPointsStayUnreachable(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files["internal/service/http.go"] == nil || files["cmd/trimlight/main.go"] == nil {
		t.Fatal("scan missed the route table or the CLI", len(files))
	}
	violations, defined := dormancyScan(files, fset)
	for _, v := range violations {
		t.Errorf("non-test code reaches a dormant calendar entry point: %s", v)
	}
	// A rename must not leave the guard watching names that no longer exist.
	for name := range dormantEntries {
		if !defined[name] {
			t.Errorf("guarded definition %s was not found; update the allowed list", name)
		}
	}
}

func TestDormancyScanFlagsReachingCode(t *testing.T) {
	sources := map[string]string{
		// Allowed definitions, as in the real files, plus a new helper there.
		"internal/service/calendar_runner.go": `package service
type calendarRunner struct{}
func newCalendarRunner() *calendarRunner { return &calendarRunner{} }
func (r *calendarRunner) Run() {}
func startCalendar() { newCalendarRunner().Run() }`,
		"internal/service/calendar_handoff.go": `package service
type calendarHandoff struct{}
func newCalendarHandoff() *calendarHandoff { return &calendarHandoff{} }`,
		"internal/storage/calendar_runtime.go": `package storage
type Store struct{}
func (s *Store) ClaimCalendar() error { return nil }`,
		// A route that reaches ownership and the handoff coordinator.
		"internal/service/http.go": `package service
func routes(mux muxer, s server) {
	mux.HandleFunc("POST /api/v1/calendar/activate", func(w writer, r request) { _ = s.store.ClaimCalendar() })
	mux.HandleFunc("POST /api/v1/calendar/handoff", func(w writer, r request) { _ = &calendarHandoff{} })
}`,
		// CLI wiring that keeps a function value and shadows the store method.
		"cmd/trimlight/service.go": `package main
var start = newCalendarRunner
func (s *Store) ClaimCalendar() {}`,
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for path, src := range sources {
		file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = file
	}
	violations, defined := dormancyScan(files, fset)
	want := []string{
		"cmd/trimlight/service.go:2: newCalendarRunner",
		"cmd/trimlight/service.go:3: ClaimCalendar",
		"internal/service/calendar_runner.go:5: newCalendarRunner",
		"internal/service/http.go:3: ClaimCalendar",
		"internal/service/http.go:4: calendarHandoff",
	}
	if !reflect.DeepEqual(violations, want) {
		t.Fatalf("violations %q, want %q", violations, want)
	}
	if len(defined) != len(dormantEntries) {
		t.Fatal("allowed definitions were not recognized", defined)
	}
}
