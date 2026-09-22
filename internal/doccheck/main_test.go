package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	src := `// Package sample is documented.
package sample

// Good is fine.
type Good struct{}

// The Article form is fine too.
type Article int

type Missing struct{}

// wrong start.
func Bad() {}

// Good documents itself.
func (Good) Good() {}

// Method on an unexported type does not show; wording is free.
func (hidden) Whatever() {}

type hidden struct{}

// Grouped constants may share one comment.
const (
	A = 1
	B = 2
)

var Loose = 3

var (
	// Documented explains itself.
	Documented = 4
	// Misnamed says the wrong name.
	Other = 5
)

func unexported() {}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := check(pkg{Dir: dir, ImportPath: "sample", Name: "sample", GoFiles: []string{"sample.go"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exported Missing has no doc comment",
		`doc comment of Bad should start with "Bad"`,
		"exported Loose has no doc comment",
		`doc comment of Other should start with "Other"`,
	}
	if len(found) != len(want) {
		t.Fatalf("got %d problems, want %d:\n%s", len(found), len(want), strings.Join(found, "\n"))
	}
	for i, w := range want {
		if !strings.HasSuffix(found[i], w) {
			t.Errorf("problem %d: got %q, want suffix %q", i, found[i], w)
		}
	}
}

func TestPackageComment(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.go": "package nodoc\n\n// Exported is fine.\nfunc Exported() {}\n",
		"b.go": "// Wrong opening line.\npackage nodoc\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	found, err := check(pkg{Dir: dir, Name: "nodoc", GoFiles: []string{"a.go", "b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !strings.Contains(found[0], `should start with "Package nodoc"`) {
		t.Errorf("got %q", found)
	}

	found, err = check(pkg{Dir: dir, Name: "nodoc", GoFiles: []string{"a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !strings.Contains(found[0], "has no package comment") {
		t.Errorf("got %q", found)
	}
}

// The repository itself must pass, which is what CI relies on.
func TestRepository(t *testing.T) {
	t.Chdir("../..")
	pkgs, err := list([]string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 4 {
		t.Fatalf("resolved only %d packages", len(pkgs))
	}
	for _, p := range pkgs {
		found, err := check(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			t.Error(f)
		}
	}
}
