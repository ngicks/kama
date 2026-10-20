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

func targetNode() *fakeNode {
	return &fakeNode{
		provides: []KeyRef{Key[*target]{}},
		run: func(_ *Inputs, out *Outputs) error {
			return out.Set(&target{})
		},
	}
}

// source reads the target and provides src with a value that changes on every
// run, so a second instance is observable downstream.
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

// output writes into the target and provides it again.
func output(name string) *fakeNode {
	return &fakeNode{
		requires: []KeyRef{Key[*target]{}, Key[src]{}},
		provides: []KeyRef{Key[*target]{}},
		run: func(in *Inputs, out *Outputs) error {
			t, err := in.Get[*target]()
			if err != nil {
				return err
			}

			t.written = append(t.written, name+".go")
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
	assert.NilError(t, g.Add("fs", targetNode()))
	assert.NilError(t, g.Add("src", s, "fs"))
	assert.NilError(t, g.Add("dbl", doubler(), "src"))
	assert.NilError(t, g.Add("c", consumer(&seenSrc), "src"))
	assert.NilError(t, g.Add("join", join, "src", "dbl"))
	assert.NilError(t, g.Run(context.Background()))

	assert.Equal(t, s.runs, 1)
	assert.DeepEqual(t, seenSrc, []src{1})
	assert.DeepEqual(t, seen, []int{1, 2})
}

func TestExplicitReload(t *testing.T) {
	var seen []double
	var targets []*target
	first := source(1)
	second := source(2)

	g := NewGraph()
	assert.NilError(t, g.Add("fs", targetNode()))
	assert.NilError(t, g.Add("src1", first, "fs"))
	assert.NilError(t, g.Add("dbl1", doubler(), "src1"))
	assert.NilError(t, g.Add("before", consumer(&seen), "dbl1"))
	assert.NilError(t, g.Add("out", output("gen"), "fs", "src1"))
	assert.NilError(t, g.Add("src2", second, "out"))
	assert.NilError(t, g.Add("dbl2", doubler(), "src2"))
	assert.NilError(t, g.Add("after", consumer(&seen), "dbl2"))
	assert.NilError(t, g.Add("target", consumer(&targets), "out"))
	assert.NilError(t, g.Run(context.Background()))

	assert.Equal(t, first.runs, 1)
	assert.Equal(t, second.runs, 1)
	assert.DeepEqual(t, seen, []double{2, 4})
	assert.Equal(t, len(targets), 1)
	assert.DeepEqual(t, targets[0].written, []string{"gen.go"})
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
	assert.NilError(t, g.Add("fs", targetNode()))
	assert.NilError(t, g.Add("src", source(1), "fs"))
	assert.NilError(t, g.Add("a", consumer(&seen), "src"))
	assert.NilError(t, g.Add("b", consumer(&seen), "src"))

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
	assert.NilError(t, g.Add("fs", targetNode()))
	assert.NilError(t, g.Add("src1", source(1), "fs"))
	assert.NilError(t, g.Add("src2", source(2), "fs"))

	assert.ErrorIs(t, g.Add("fs", targetNode()), ErrDuplicateNode)
	assert.ErrorIs(t, g.Add("c", consumer(&seen), "nope"), ErrUnknownNode)
	assert.ErrorIs(t, g.Add("c", consumer(&seen)), ErrNoProvider)
	assert.ErrorIs(t, g.Add("c", consumer(&seen), "src1", "src2"), ErrAmbiguousProvider)

	// Only dependencies resolve types: fs provides nothing a consumer of src
	// wants, even though src1 is in the graph.
	assert.ErrorIs(t, g.Add("c", consumer(&seen), "fs"), ErrNoProvider)
}

func TestRunErrors(t *testing.T) {
	bad := &fakeNode{provides: []KeyRef{Key[src]{}}}

	g := NewGraph()
	assert.NilError(t, g.Add("bad", bad))
	assert.ErrorIs(t, g.Run(context.Background()), ErrMissingOutput)

	undeclared := &fakeNode{run: func(_ *Inputs, out *Outputs) error { return out.Set(src(1)) }}

	g = NewGraph()
	assert.NilError(t, g.Add("undeclared", undeclared))
	assert.ErrorIs(t, g.Run(context.Background()), ErrNotDeclared)

	notRequired := &fakeNode{
		requires: []KeyRef{Key[src]{}},
		run: func(in *Inputs, _ *Outputs) error {
			_, err := in.Get[double]()
			return err
		},
	}

	g = NewGraph()
	assert.NilError(t, g.Add("fs", targetNode()))
	assert.NilError(t, g.Add("src", source(1), "fs"))
	assert.NilError(t, g.Add("nr", notRequired, "src"))
	assert.ErrorIs(t, g.Run(context.Background()), ErrNotInRequires)
}
