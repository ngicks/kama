package validation

import (
	"iter"
	"maps"
	"slices"
	"strings"
)

// At returns the leaves of err located exactly at pointer, in input order.
func At(err error, pointer string) []error {
	var out []error

	for p, e := range leaves(err) {
		if p == pointer {
			out = append(out, e)
		}
	}

	return out
}

// Under returns the leaves of err located at pointer or below it, in input order. The root
// pointer "" selects every leaf.
func Under(err error, pointer string) []error {
	var out []error

	for p, e := range leaves(err) {
		if p == pointer || strings.HasPrefix(p, pointer+"/") {
			out = append(out, e)
		}
	}

	return out
}

// Pointers returns the distinct locations of the leaves of err, sorted lexically.
func Pointers(err error) []string {
	set := map[string]struct{}{}
	for p := range leaves(err) {
		set[p] = struct{}{}
	}

	return slices.Sorted(maps.Keys(set))
}

// leaves yields every leaf of err with its pointer. Join keeps its values flat, so the
// recursion only matters for a joinError built some other way inside this package.
func leaves(err error) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		walkLeaves(err, yield)
	}
}

func walkLeaves(err error, yield func(string, error) bool) bool {
	switch x := err.(type) {
	case nil:
		return true
	case *joinError:
		for _, e := range x.errs {
			if !walkLeaves(e, yield) {
				return false
			}
		}

		return true
	default:
		return yield(pointerOf(err), err)
	}
}
