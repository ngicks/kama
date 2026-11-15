package base

import (
	"maps"
	"slices"

	"github.com/ngicks/opiter"
)

// Caller is a generated file calling declarations its package shares.
type Caller struct {
	// Constrained reports whether the source file has a build constraint,
	// as a //go:build line or a file name suffix.
	Constrained bool
	// BuildLine is the //go:build line of the source file, or empty.
	BuildLine string
	// Own is the path of the file declaring what only constrained callers
	// use, when this caller is the first of them.
	Own string
	// Uses are the names of the shared declarations the file calls.
	Uses map[string]bool
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
// given, use, sorted by path. Every name is declared once.
//
// A name a caller without a build constraint uses goes to shared. Such a
// caller is in every build variant, and what it uses does not depend on
// the variant loaded, so shared comes out the same whichever variant a run
// loads.
//
// A name only constrained callers use cannot go there: its types may not
// exist in another variant, and a run loading another variant would drop
// it. It goes to the Own file of the first caller using it, under the build
// line of that caller, which stays right as long as the callers using the
// name are loaded together.
func HostShared(shared string, callers []Caller) []Host {
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
		usesName := func(c Caller) bool { return c.Uses[name] }
		users := opiter.Filter(usesName, slices.Values(callers))
		if opiter.ContainsFunc(isUnconstrained, users) {
			host(shared, "", name)
		} else {
			first := opiter.First(users).Value()
			host(first.Own, first.BuildLine, name)
		}
	}
	return slices.Collect(opiter.Map(hostValue, opiter.OmitF(opiter.MapsSorted(byPath))))
}

func isUnconstrained(c Caller) bool { return !c.Constrained }

func hostValue(h *Host) Host { return *h }
