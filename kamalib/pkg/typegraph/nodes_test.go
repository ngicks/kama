package typegraph

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"gotest.tools/v3/assert"
)

func typeNames(nodes []*Node) []string {
	return opiter.WrapSeq(slices.Values(nodes)).
		Map(func(n *Node) string { return n.Id.TypeName }).
		CollectSlice()
}

func filesGraph(t *testing.T) (*Graph, string) {
	t.Helper()

	pkgs := loadPackages(t, "./testdata/files")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	return g, pkgPathOf(t, pkgs, "files")
}

func TestNodeFilename(t *testing.T) {
	g, pkgPath := filesGraph(t)

	n, ok := g.Get(NewQualifiedType(pkgPath, "Third"))
	assert.Assert(t, ok)
	assert.Equal(t, "b.go", filepath.Base(n.Filename()))
}

func TestSortedNodes(t *testing.T) {
	g, _ := filesGraph(t)

	assert.DeepEqual(t,
		[]string{"First", "Second", "Third"},
		typeNames(SortedNodes(maps.Values(maps.Collect(g.All())))),
	)
}

func TestNodesIn(t *testing.T) {
	g, pkgPath := filesGraph(t)

	third, ok := g.Get(NewQualifiedType(pkgPath, "Third"))
	assert.Assert(t, ok)

	a := filepath.Join(filepath.Dir(third.Filename()), "a.go")
	nodes := maps.Values(maps.Collect(g.All()))

	assert.DeepEqual(t, []string{"Second", "First"}, typeNames(NodesIn(nodes, a)))
	assert.DeepEqual(t, []string{"Third"}, typeNames(NodesIn(nodes, third.Filename())))
	assert.Equal(t, 0, len(NodesIn(nodes, "missing.go")))
}

func TestLookupType(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/files")
	pkgPath := pkgPathOf(t, pkgs, "files")

	obj := LookupType(pkgs, NewQualifiedType(pkgPath, "First"))
	assert.Assert(t, obj != nil)
	assert.Equal(t, "First", obj.Name())

	assert.Assert(t, LookupType(pkgs, NewQualifiedType(pkgPath, "Missing")) == nil)
	assert.Assert(t, LookupType(pkgs, NewQualifiedType("example.com/missing", "First")) == nil)
}
