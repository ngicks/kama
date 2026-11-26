package unknownkey

import (
	"slices"

	"github.com/ngicks/opiter"
)

// Paths returns the keys of unknown, the members a decoded table captures,
// as key paths below prefix in key order.
func Paths(unknown map[string]any, prefix ...string) [][]string {
	return opiter.MapsSorted(unknown).
		OmitL().
		Map(func(k string) []string { return append(slices.Clip(prefix), k) }).
		CollectSlice()
}

// Under returns paths, the key paths of a nested table, below prefix.
func Under(paths [][]string, prefix ...string) [][]string {
	return opiter.SlicesValues(paths).
		Map(func(p []string) []string { return slices.Concat(prefix, p) }).
		CollectSlice()
}
