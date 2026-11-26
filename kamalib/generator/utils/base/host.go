package base

import (
	"go/ast"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
)

// Caller is a generated file calling declarations its package shares. It
// names the source file the generated file is made from. HostShared reads
// the build constraint of the caller from there.
type Caller struct {
	// Src is the path of the source file, and File its syntax tree.
	Src  string
	File *ast.File
	// Uses are the names of the shared declarations the file calls. No
	// source holds them: they tell what the generator emitted.
	Uses map[string]bool
}

// constrained reports whether the source file has a build constraint the
// file shared stands under lacks: a //go:build line, a file name suffix,
// or the _test suffix when shared is no test file.
func (c Caller) constrained(shared string) bool {
	if _, ok := BuildConstraint(c.File); ok {
		return true
	}

	src, test := strings.CutSuffix(c.Src, "_test.go")
	if !test {
		return constrained(c.Src)
	}

	// constrained takes the _test suffix for a constraint of its own.
	return constrained(src+".go") || !isTestFile(shared)
}

// isTestFile reports whether the file at path is a _test.go file.
func isTestFile(path string) bool { return strings.HasSuffix(path, "_test.go") }

// Host is a file declaring shared declarations.
type Host struct {
	Path string
	// BuildLine is the //go:build line of the file, or empty.
	BuildLine string
	// Names are the declarations the file holds, sorted.
	Names []string
}

// HostShared returns the files declaring what callers, in the order
// given, use, sorted by path. Every name is declared once. n and kind name
// the files, as Naming.SharedFileOf does.
//
// A name a caller without a build constraint uses goes to shared. Such a
// caller is in every build variant, and what it uses does not depend on
// the variant loaded, so shared comes out the same whichever variant a run
// loads.
//
// A name only constrained callers use cannot go there: its types may not
// exist in another variant, and a run loading another variant would drop
// it. It goes to the file Naming.SharedFileOf names for the source file of
// the first caller using it, under the build
// line of that caller, which stays right as long as the callers using the
// name are loaded together.
//
// A _test.go caller counts as constrained unless shared is a _test.go file
// too, as that of an external test package is: a name only test files use
// stays out of the build of the package. The first caller that is a test
// file exactly when shared is one is preferred to host a name, so a name a
// test file and a constrained file of the package both use goes where the
// build of the package sees it.
func HostShared(n Naming, kind, shared string, callers []Caller) []Host {
	names := map[string]bool{}
	for _, c := range callers {
		maps.Copy(names, c.Uses)
	}

	byPath := map[string]*Host{}

	host := func(path, buildLine, name string) {
		h := byPath[path]
		if h == nil {
			h = &Host{Path: path, BuildLine: buildLine}
			byPath[path] = h
		}

		h.Names = append(h.Names, name)
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		users := opiter.SlicesValues(callers).
			Filter(func(c Caller) bool { return c.Uses[name] })

		if users.ContainsFunc(func(c Caller) bool { return !c.constrained(shared) }) {
			host(shared, "", name)
		} else {
			first := users.
				Filter(func(c Caller) bool { return isTestFile(c.Src) == isTestFile(shared) }).
				First().
				Or(users.First()).
				Value()
			buildLine, _ := BuildConstraint(first.File)
			host(n.SharedFileOf(kind, first.Src), buildLine, name)
		}
	}

	return opiter.MapsSorted(byPath).
		OmitF().
		Map(func(h *Host) Host { return *h }).
		CollectSlice()
}
