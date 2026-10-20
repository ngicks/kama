package workflow

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
)

type fakeNode struct {
	requires []KeyRef
	provides []KeyRef
	run      func(in *Inputs, out *Outputs) error
	runs     int
}

func (n *fakeNode) Requires() []KeyRef { return n.requires }
func (n *fakeNode) Provides() []KeyRef { return n.provides }
func (n *fakeNode) Run(_ context.Context, in *Inputs, out *Outputs) error {
	n.runs++

	if n.run == nil {
		return nil
	}

	return n.run(in, out)
}

// target stands in for the target filesystem: a value a source reads through
// and an output node provides again after writing.
type target struct{ written []string }

func (*target) DefaultNode() Node { return nil }

// src and double have no default: a dependency must provide them.
type (
	src    int
	double int
)

func (src) DefaultNode() Node    { return nil }
func (double) DefaultNode() Node { return nil }

func deps(names ...string) Edges { return Edges{Deps: names} }

func targetNode() *fakeNode {
	return &fakeNode{
		provides: []KeyRef{Key[*target]{}},
		run: func(_ *Inputs, out *Outputs) error {
			return out.Set(&target{})
		},
	}
}

// source reads the target and provides src with a fixed value, so a second
// instance is observable downstream.
func source(v int) *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[*target]{}},
		provides: []KeyRef{Key[src]{}},
		run: func(_ *Inputs, out *Outputs) error {
			return out.Set(src(v))
		},
	}
}

func doubler() *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[src]{}},
		provides: []KeyRef{Key[double]{}},
		run: func(in *Inputs, out *Outputs) error {
			v, err := in.Get[src]()
			if err != nil {
				return err
			}

			return out.Set(double(v * 2))
		},
	}
}

// generator emits one file named after the src value it reads.
func generator(prefix string) *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[src]{}},
		run: func(in *Inputs, out *Outputs) error {
			v, err := in.Get[src]()
			if err != nil {
				return err
			}

			return out.Emit(prefix+".gen.go", []byte{byte(v)})
		},
	}
}

// sink writes routed files into the target and provides it again.
func sink() *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[*target]{}},
		provides: []KeyRef{Key[*target]{}},
		run: func(in *Inputs, out *Outputs) error {
			t, err := in.Get[*target]()
			if err != nil {
				return err
			}

			for _, f := range in.Emitted() {
				t.written = append(t.written, f.Path)
			}

			return out.Set(t)
		},
	}
}

func consumer[T Value](seen *[]T) *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[T]{}},
		run: func(in *Inputs, _ *Outputs) error {
			v, err := in.Get[T]()
			if err != nil {
				return err
			}

			*seen = append(*seen, v)
			return nil
		},
	}
}

func TestBranchAndJoin(t *testing.T) {
	var seen []int
	var seenSrc []src

	s := source(1)
	join := &fakeNode{
		requires: []KeyRef{Key[src]{}, Key[double]{}},
		run: func(in *Inputs, _ *Outputs) error {
			a, _ := in.Get[src]()
			b, _ := in.Get[double]()
			seen = append(seen, int(a), int(b))
			return nil
		},
	}

	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src", s, deps("fs")))
	assert.NilError(t, g.Add("dbl", doubler(), deps("src")))
	assert.NilError(t, g.Add("c", consumer(&seenSrc), deps("src")))
	assert.NilError(t, g.Add("join", join, deps("src", "dbl")))
	assert.NilError(t, g.Run(context.Background()))

	assert.Equal(t, s.runs, 1)
	assert.DeepEqual(t, seenSrc, []src{1})
	assert.DeepEqual(t, seen, []int{1, 2})
}

func TestOutputRouting(t *testing.T) {
	var seen []double
	var targets []*target

	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src1", source(1), deps("fs")))
	assert.NilError(t, g.Add("a", generator("a"), Edges{Deps: []string{"src1"}, Output: "out"}))
	assert.NilError(t, g.Add("b", generator("b"), Edges{Deps: []string{"src1"}, Output: "out"}))
	assert.NilError(t, g.Add("out", sink(), deps("fs")))
	assert.NilError(t, g.Add("src2", source(2), deps("out")))
	assert.NilError(t, g.Add("dbl", doubler(), deps("src2")))
	assert.NilError(t, g.Add("after", consumer(&seen), deps("dbl")))
	assert.NilError(t, g.Add("target", consumer(&targets), deps("out")))

	assert.DeepEqual(t, g.Names(), []string{
		"fs", "src1", "a", "b", "out", "src2", "dbl", "after", "target",
	})

	assert.NilError(t, g.Run(context.Background()))

	assert.DeepEqual(t, seen, []double{4})
	assert.Equal(t, len(targets), 1)
	assert.DeepEqual(t, targets[0].written, []string{"a.gen.go", "b.gen.go"})
}

func TestOutputErrors(t *testing.T) {
	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src", source(1), deps("fs")))

	// An output must come after the nodes routed to it.
	assert.NilError(t, g.Add("early", sink(), deps("fs")))
	err := g.Add("g", generator("g"), Edges{Deps: []string{"src"}, Output: "early"})
	assert.ErrorIs(t, err, ErrUnknownNode)

	// Emitting without a declared output fails at run time.
	assert.NilError(t, g.Add("noout", generator("noout"), deps("src")))
	assert.ErrorIs(t, g.Run(context.Background()), ErrNoOutputNode)

	// An output declared but never added fails before anything runs.
	g = NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src", source(1), deps("fs")))
	assert.NilError(t, g.Add("g", generator("g"), Edges{Deps: []string{"src"}, Output: "never"}))
	assert.ErrorIs(t, g.Run(context.Background()), ErrUnknownNode)
}

// withDefault loads itself when no dependency provides it.
type withDefault int

var defaultsMade []*fakeNode

func (withDefault) DefaultNode() Node {
	n := &fakeNode{
		requires: []KeyRef{Key[src]{}},
		provides: []KeyRef{Key[withDefault]{}},
		run: func(in *Inputs, out *Outputs) error {
			v, err := in.Get[src]()
			if err != nil {
				return err
			}

			return out.Set(withDefault(v + 6))
		},
	}

	defaultsMade = append(defaultsMade, n)
	return n
}

func TestDefaultResolvesAgainstSameDeps(t *testing.T) {
	defaultsMade = nil
	var seen []withDefault

	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src", source(1), deps("fs")))
	assert.NilError(t, g.Add("a", consumer(&seen), deps("src")))
	assert.NilError(t, g.Add("b", consumer(&seen), deps("src")))

	assert.DeepEqual(t, g.Names(), []string{
		"fs", "src", "a/workflow.withDefault", "a", "b/workflow.withDefault", "b",
	})

	assert.NilError(t, g.Run(context.Background()))

	assert.Equal(t, len(defaultsMade), 2)
	assert.DeepEqual(t, seen, []withDefault{7, 7})
}

func TestAddErrors(t *testing.T) {
	var seen []src

	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src1", source(1), deps("fs")))
	assert.NilError(t, g.Add("src2", source(2), deps("fs")))

	assert.ErrorIs(t, g.Add("fs", targetNode(), Edges{}), ErrDuplicateNode)
	assert.ErrorIs(t, g.Add("c", consumer(&seen), deps("nope")), ErrUnknownNode)
	assert.ErrorIs(t, g.Add("c", consumer(&seen), Edges{}), ErrNoProvider)
	assert.ErrorIs(t, g.Add("c", consumer(&seen), deps("src1", "src2")), ErrAmbiguousProvider)

	// Only dependencies resolve types: fs provides nothing a consumer of src
	// wants, even though src1 is in the graph.
	assert.ErrorIs(t, g.Add("c", consumer(&seen), deps("fs")), ErrNoProvider)
}

func TestRunErrors(t *testing.T) {
	bad := &fakeNode{provides: []KeyRef{Key[src]{}}}

	g := NewGraph()
	assert.NilError(t, g.Add("bad", bad, Edges{}))
	assert.ErrorIs(t, g.Run(context.Background()), ErrMissingOutput)

	undeclared := &fakeNode{run: func(_ *Inputs, out *Outputs) error { return out.Set(src(1)) }}

	g = NewGraph()
	assert.NilError(t, g.Add("undeclared", undeclared, Edges{}))
	assert.ErrorIs(t, g.Run(context.Background()), ErrNotDeclared)

	notRequired := &fakeNode{
		requires: []KeyRef{Key[src]{}},
		run: func(in *Inputs, _ *Outputs) error {
			_, err := in.Get[double]()
			return err
		},
	}

	g = NewGraph()
	assert.NilError(t, g.Add("fs", targetNode(), Edges{}))
	assert.NilError(t, g.Add("src", source(1), deps("fs")))
	assert.NilError(t, g.Add("nr", notRequired, deps("src")))
	assert.ErrorIs(t, g.Run(context.Background()), ErrNotInRequires)
}
