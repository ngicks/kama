package workflow

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
)

type fakeNode struct {
	name     string
	requires []KeyRef
	provides []KeyRef
	run      func(in *Inputs, out *Outputs) error
	runs     int
}

func (n *fakeNode) Name() string       { return n.name }
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

// src and double have no default: a pipeline must provide them.
type (
	src    int
	double int
)

func (src) DefaultNode() Node    { return nil }
func (double) DefaultNode() Node { return nil }

func targetNode() *fakeNode {
	return &fakeNode{
		name:     "target",
		provides: []KeyRef{Key[*target]{}},
		run: func(_ *Inputs, out *Outputs) error {
			return out.Set(&target{})
		},
	}
}

// source reads the target and provides src with a value that changes on every
// run, so a rerun is observable downstream.
func source() *fakeNode {
	n := &fakeNode{
		name:     "source",
		requires: []KeyRef{Key[*target]{}},
		provides: []KeyRef{Key[src]{}},
	}
	n.run = func(_ *Inputs, out *Outputs) error {
		return out.Set(src(n.runs))
	}

	return n
}

func doubler() *fakeNode {
	return &fakeNode{
		name:     "double",
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
		name:     name,
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

func consumer[T Value](name string, seen *[]T) *fakeNode {
	return &fakeNode{
		name:     name,
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

func mustRun(t *testing.T, nodes ...Node) {
	t.Helper()

	p, err := New(nodes...)
	assert.NilError(t, err)
	assert.NilError(t, p.Run(context.Background()))
}

func TestBranchAndJoin(t *testing.T) {
	var seen []int
	var seenSrc []src

	s := source()
	join := &fakeNode{
		name:     "join",
		requires: []KeyRef{Key[src]{}, Key[double]{}},
		run: func(in *Inputs, _ *Outputs) error {
			a, _ := in.Get[src]()
			b, _ := in.Get[double]()
			seen = append(seen, int(a), int(b))
			return nil
		},
	}

	mustRun(t, targetNode(), s, doubler(), consumer("c", &seenSrc), join)

	assert.Equal(t, s.runs, 1)
	assert.DeepEqual(t, seenSrc, []src{1})
	assert.DeepEqual(t, seen, []int{1, 2})
}

func TestReloadAfterOutput(t *testing.T) {
	var seen []double
	var targets []*target
	s := source()
	dbl := doubler()

	mustRun(t,
		targetNode(),
		s,
		dbl,
		consumer("before", &seen),
		output("gen"),
		consumer("after", &seen),
		consumer("again", &seen),
		consumer("target", &targets),
	)

	assert.Equal(t, s.runs, 2)
	assert.Equal(t, dbl.runs, 2)
	assert.DeepEqual(t, seen, []double{2, 4, 4})
	assert.Equal(t, len(targets), 1)
	assert.DeepEqual(t, targets[0].written, []string{"gen.go"})
}

func TestNearestEarlierProvider(t *testing.T) {
	var seen []src
	override := &fakeNode{
		name:     "override",
		provides: []KeyRef{Key[src]{}},
		run: func(_ *Inputs, out *Outputs) error {
			return out.Set(src(100))
		},
	}

	mustRun(t, targetNode(), source(), consumer("a", &seen), override, consumer("b", &seen))

	assert.DeepEqual(t, seen, []src{1, 100})
}

// withDefault loads itself when no node provides it.
type withDefault int

var defaultsMade []*fakeNode

func (withDefault) DefaultNode() Node {
	n := &fakeNode{name: "default", provides: []KeyRef{Key[withDefault]{}}}
	n.run = func(_ *Inputs, out *Outputs) error {
		return out.Set(withDefault(7))
	}

	defaultsMade = append(defaultsMade, n)
	return n
}

func TestDefaultInsertedOnce(t *testing.T) {
	defaultsMade = nil
	var seen []withDefault

	p, err := New(consumer("a", &seen), consumer("b", &seen))
	assert.NilError(t, err)

	names := make([]string, 0, 3)
	for _, n := range p.Nodes() {
		names = append(names, n.Name())
	}
	assert.DeepEqual(t, names, []string{"default", "a", "b"})

	assert.NilError(t, p.Run(context.Background()))

	assert.Equal(t, len(defaultsMade), 1)
	assert.Equal(t, defaultsMade[0].runs, 1)
	assert.DeepEqual(t, seen, []withDefault{7, 7})
}

// ping and pong are mutually derived, which only a cycle through time can
// satisfy.
type (
	ping int
	pong int
)

func (ping) DefaultNode() Node { return nil }
func (pong) DefaultNode() Node { return nil }

func TestCycle(t *testing.T) {
	seed := &fakeNode{
		name:     "seed",
		provides: []KeyRef{Key[ping]{}},
		run:      func(_ *Inputs, out *Outputs) error { return out.Set(ping(0)) },
	}
	toPong := &fakeNode{
		name:     "topong",
		requires: []KeyRef{Key[ping]{}},
		provides: []KeyRef{Key[pong]{}},
		run:      func(_ *Inputs, out *Outputs) error { return out.Set(pong(0)) },
	}
	toPing := &fakeNode{
		name:     "toping",
		requires: []KeyRef{Key[pong]{}},
		provides: []KeyRef{Key[ping]{}},
		run:      func(_ *Inputs, out *Outputs) error { return out.Set(ping(0)) },
	}
	var seen []pong

	p, err := New(seed, toPong, toPing, consumer("c", &seen))
	assert.NilError(t, err)

	assert.ErrorIs(t, p.Run(context.Background()), ErrCycle)
}

func TestErrors(t *testing.T) {
	var seen []src

	_, err := New(consumer("a", &seen))
	assert.ErrorIs(t, err, ErrNoProvider)

	bad := &fakeNode{name: "bad", provides: []KeyRef{Key[src]{}}}

	p, err := New(bad)
	assert.NilError(t, err)
	assert.ErrorIs(t, p.Run(context.Background()), ErrMissingOutput)

	undeclared := &fakeNode{name: "undeclared"}
	undeclared.run = func(_ *Inputs, out *Outputs) error { return out.Set(src(1)) }

	p, err = New(undeclared)
	assert.NilError(t, err)
	assert.ErrorIs(t, p.Run(context.Background()), ErrNotDeclared)

	notRequired := &fakeNode{name: "notrequired", requires: []KeyRef{Key[src]{}}}
	notRequired.run = func(in *Inputs, _ *Outputs) error {
		_, err := in.Get[double]()
		return err
	}

	p, err = New(targetNode(), source(), notRequired)
	assert.NilError(t, err)
	assert.ErrorIs(t, p.Run(context.Background()), ErrNotInRequires)
}
