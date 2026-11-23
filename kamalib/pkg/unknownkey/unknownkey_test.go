package unknownkey

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestPaths(t *testing.T) {
	unknown := map[string]any{"b": 1, "a": nil, "c": map[string]any{"d": true}}

	assert.DeepEqual(t, Paths(unknown), [][]string{{"a"}, {"b"}, {"c"}})
	assert.DeepEqual(t, Paths(unknown, "x", "0"), [][]string{
		{"x", "0", "a"},
		{"x", "0", "b"},
		{"x", "0", "c"},
	})
	assert.Equal(t, len(Paths(nil, "x")), 0)
}

// TestPathsKeepsThePrefix checks that a prefix with spare capacity is not
// written into, so the paths do not share it.
func TestPathsKeepsThePrefix(t *testing.T) {
	prefix := make([]string, 1, 4)
	prefix[0] = "x"

	paths := Paths(map[string]any{"a": 1, "b": 2}, prefix...)

	assert.DeepEqual(t, paths, [][]string{{"x", "a"}, {"x", "b"}})
	assert.DeepEqual(t, prefix[:cap(prefix)], []string{"x", "", "", ""})
}

func TestUnder(t *testing.T) {
	assert.DeepEqual(
		t,
		Under([][]string{{"a"}, {"b", "c"}}, "x", "y"),
		[][]string{{"x", "y", "a"}, {"x", "y", "b", "c"}},
	)
	assert.DeepEqual(t, Under([][]string{{"a"}}), [][]string{{"a"}})
	assert.Equal(t, len(Under(nil, "x")), 0)
}
