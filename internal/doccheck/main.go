// Command doccheck reports exported identifiers without a doc comment, doc
// comments that do not start with the identifier's name, and packages without
// a package comment. go vet and staticcheck do not check these, and they are
// what pkg.go.dev shows.
//
//	go run ./internal/doccheck ./...
//
// Test files are skipped. A const or var block may carry one comment for the
// whole group. It exits 1 when it finds anything.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type pkg struct {
	Dir        string
	ImportPath string
	Name       string
	GoFiles    []string
}

func main() {
	patterns := os.Args[1:]
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	pkgs, err := list(patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "doccheck:", err)
		os.Exit(2)
	}
	problems := 0
	for _, p := range pkgs {
		found, err := check(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "doccheck:", err)
			os.Exit(2)
		}
		for _, f := range found {
			fmt.Println(f)
		}
		problems += len(found)
	}
	if problems > 0 {
		fmt.Fprintf(os.Stderr, "doccheck: %d problem(s)\n", problems)
		os.Exit(1)
	}
}

// list resolves package patterns with the go command, so that build
// constraints and test files are handled the way go doc handles them.
func list(patterns []string) ([]pkg, error) {
	args := append([]string{"list", "-json=Dir,ImportPath,Name,GoFiles"}, patterns...)
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// check parses the non-test files of one package and returns one line per
// problem, as "path:line: message".
func check(p pkg) ([]string, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range p.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	var found []string
	report := func(pos token.Pos, format string, args ...any) {
		found = append(found, fmt.Sprintf("%s: %s", fset.Position(pos), fmt.Sprintf(format, args...)))
	}

	hasPackageDoc := false
	for _, f := range files {
		if f.Doc == nil {
			continue
		}
		hasPackageDoc = true
		want := "Package " + p.Name
		if p.Name == "main" {
			want = "Command "
		}
		if text := f.Doc.Text(); !strings.HasPrefix(text, want) {
			report(f.Doc.Pos(), "package comment should start with %q", want)
		}
	}
	if !hasPackageDoc && len(files) > 0 {
		report(files[0].Package, "package %s has no package comment", p.Name)
	}

	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !exportedFunc(d) {
					continue
				}
				checkDoc(report, d.Name.Pos(), d.Name.Name, d.Doc)
			case *ast.GenDecl:
				checkGenDecl(report, d)
			}
		}
	}
	return found, nil
}

// exportedFunc reports whether a function or method shows up in the package's
// documentation: an exported name on an exported receiver type, if any.
func exportedFunc(d *ast.FuncDecl) bool {
	if !d.Name.IsExported() {
		return false
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return true
	}
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if idx, ok := expr.(*ast.IndexExpr); ok { // generic receiver
		expr = idx.X
	}
	if idx, ok := expr.(*ast.IndexListExpr); ok {
		expr = idx.X
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.IsExported()
}

func checkGenDecl(report func(token.Pos, string, ...any), d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			doc := s.Doc
			if doc == nil && len(d.Specs) == 1 {
				doc = d.Doc
			}
			checkDoc(report, s.Name.Pos(), s.Name.Name, doc)
		case *ast.ValueSpec:
			for _, name := range s.Names {
				if !name.IsExported() {
					continue
				}
				switch {
				case s.Doc != nil:
					checkDoc(report, name.Pos(), name.Name, s.Doc)
				case d.Doc != nil:
					// A group comment covers every name in the block; its
					// wording is free.
				default:
					report(name.Pos(), "exported %s has no doc comment", name.Name)
				}
			}
		}
	}
}

// checkDoc applies the go doc convention: a comment exists and its first word
// is the identifier, optionally after "A", "An" or "The".
func checkDoc(report func(token.Pos, string, ...any), pos token.Pos, name string, doc *ast.CommentGroup) {
	if doc == nil {
		report(pos, "exported %s has no doc comment", name)
		return
	}
	text := doc.Text()
	for _, article := range []string{"A ", "An ", "The "} {
		if strings.HasPrefix(text, article+name+" ") || strings.HasPrefix(text, article+name+"\n") {
			return
		}
	}
	if strings.HasPrefix(text, name+" ") || strings.HasPrefix(text, name+"\n") || strings.HasPrefix(text, name+".") || strings.HasPrefix(text, name+",") {
		return
	}
	report(pos, "doc comment of %s should start with %q", name, name)
}
