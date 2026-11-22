package base

import (
	"go/ast"
	"maps"
	"slices"

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

// constrained reports whether the source file has a build constraint, as a
// //go:build line or a file name suffix.
func (c Caller) constrained() bool {
	_, ok := BuildConstraint(c.File)
	return ok || Constrained(c.Src)
}

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
		users := opiter.WrapSeq(slices.Values(callers)).
			Filter(func(c Caller) bool { return c.Uses[name] })

		if users.ContainsFunc(func(c Caller) bool { return !c.constrained() }) {
			host(shared, "", name)
		} else {
			first := users.First().Value()
			buildLine, _ := BuildConstraint(first.File)
			host(n.SharedFileOf(kind, first.Src), buildLine, name)
		}
	}

	return opiter.WrapSeq2(opiter.MapsSorted(byPath)).
		OmitF().
		Map(func(h *Host) Host { return *h }).
		CollectSlice()
}
