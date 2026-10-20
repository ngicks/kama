package workflow

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeNode struct {
	name     string
	requires []KeyRef
	provides []KeyRef
	run      func(in *Inputs, out *Outputs, emit Emitter) error
	runs     int
}

func (n *fakeNode) Name() string       { return n.name }
func (n *fakeNode) Requires() []KeyRef { return n.requires }
func (n *fakeNode) Provides() []KeyRef { return n.provides }
func (n *fakeNode) Run(_ context.Context, in *Inputs, out *Outputs, emit Emitter) error {
	n.runs++

	if n.run == nil {
		return nil
	}

	return n.run(in, out, emit)
}

type fakeEmitter struct{ names []string }

func (e *fakeEmitter) Emit(_ context.Context, name string, _ []byte) error {
	e.names = append(e.names, name)
	return nil
}

// src and double have no default: a pipeline must provide them.
type (
	src    int
	double int
)

func (src) DefaultNode() Node    { return nil }
func (double) DefaultNode() Node { return nil }

// source provides src with a value that changes on every run, so a rerun is
// observable downstream.
func source() *fakeNode {
	n := &fakeNode{name: "source", provides: []KeyRef{Key[src]{}}}
	n.run = func(_ *Inputs, out *Outputs, _ Emitter) error {
		return out.Set(src(n.runs))
	}
	return n
}

func doubler() *fakeNode {
	return &fakeNode{
		name:     "double",
		requires: []KeyRef{Key[src]{}},
		provides: []KeyRef{Key[double]{}},
		run: func(in *Inputs, out *Outputs, _ Emitter) error {
			v, err := in.Get[src]()
			if err != nil {
				return err
			}

			return out.Set(double(v * 2))
		},
	}
}

func emitter(name string) *fakeNode {
	return &fakeNode{
		name:     name,
		requires: []KeyRef{Key[src]{}},
		run: func(_ *Inputs, _ *Outputs, emit Emitter) error {
			return emit.Emit(context.Background(), name+".go", nil)
		},
	}
}

func consumer[T Value](name string, seen *[]T) *fakeNode {
	return &fakeNode{
		name:     name,
		requires: []KeyRef{Key[T]{}},
		run: func(in *Inputs, _ *Outputs, _ Emitter) error {
			v, err := in.Get[T]()
			if err != nil {
				return err
			}

			*seen = append(*seen, v)
			return nil
		},
	}
}

func mustRun(t *testing.T, nodes ...Node) *fakeEmitter {
	t.Helper()

	p, err := New(nodes...)
	if err != nil {
		t.Fatal(err)
	}

	e := &fakeEmitter{}
	if err := p.Run(context.Background(), e); err != nil {
		t.Fatal(err)
	}

	return e
}

func TestBranchAndJoin(t *testing.T) {
	var seen []int
	var seenSrc []src

	s := source()
	join := &fakeNode{
		name:     "join",
		requires: []KeyRef{Key[src]{}, Key[double]{}},
		run: func(in *Inputs, _ *Outputs, _ Emitter) error {
			a, _ := in.Get[src]()
			b, _ := in.Get[double]()
			seen = append(seen, int(a), int(b))
			return nil
		},
	}

	mustRun(t, s, doubler(), consumer("c", &seenSrc), join)

	if s.runs != 1 {
		t.Errorf("source ran %d times, want 1", s.runs)
	}
	if want := []src{1}; !slices.Equal(seenSrc, want) {
		t.Errorf("consumer saw %v, want %v", seenSrc, want)
	}
	if want := []int{1, 2}; !slices.Equal(seen, want) {
		t.Errorf("join saw %v, want %v", seen, want)
	}
}

func TestReloadAfterEmit(t *testing.T) {
	var seen []double
	s := source()
	dbl := doubler()

	e := mustRun(t,
		s,
		dbl,
		consumer("before", &seen),
		emitter("gen"),
		consumer("after", &seen),
		consumer("again", &seen),
	)

	if want := []string{"gen.go"}; !slices.Equal(e.names, want) {
		t.Errorf("emitted %v, want %v", e.names, want)
	}
	if s.runs != 2 || dbl.runs != 2 {
		t.Errorf("source ran %d, doubler ran %d, want 2 and 2", s.runs, dbl.runs)
	}
	if want := []double{2, 4, 4}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestNearestEarlierProvider(t *testing.T) {
	var seen []src
	override := &fakeNode{
		name:     "override",
		provides: []KeyRef{Key[src]{}},
		run: func(_ *Inputs, out *Outputs, _ Emitter) error {
			return out.Set(src(100))
		},
	}

	mustRun(t, source(), consumer("a", &seen), override, consumer("b", &seen))

	if want := []src{1, 100}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

// withDefault loads itself when no node provides it.
type withDefault int

var defaultsMade []*fakeNode

func (withDefault) DefaultNode() Node {
	n := &fakeNode{name: "default", provides: []KeyRef{Key[withDefault]{}}}
	n.run = func(_ *Inputs, out *Outputs, _ Emitter) error {
		return out.Set(withDefault(7))
	}

	defaultsMade = append(defaultsMade, n)
	return n
}

func TestDefaultInsertedOnce(t *testing.T) {
	defaultsMade = nil
	var seen []withDefault

	p, err := New(consumer("a", &seen), consumer("b", &seen))
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, 3)
	for _, n := range p.Nodes() {
		names = append(names, n.Name())
	}
	if want := []string{"default", "a", "b"}; !slices.Equal(names, want) {
		t.Fatalf("planned %v, want %v", names, want)
	}

	if err := p.Run(context.Background(), &fakeEmitter{}); err != nil {
		t.Fatal(err)
	}

	if len(defaultsMade) != 1 || defaultsMade[0].runs != 1 {
		t.Errorf("default constructed %d times, want 1", len(defaultsMade))
	}
	if want := []withDefault{7, 7}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
}

func TestErrors(t *testing.T) {
	var seen []src

	_, err := New(consumer("a", &seen))
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("no provider: got %v", err)
	}

	bad := &fakeNode{name: "bad", provides: []KeyRef{Key[src]{}}}

	p, err := New(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrMissingOutput) {
		t.Errorf("missing output: got %v", err)
	}

	undeclared := &fakeNode{name: "undeclared"}
	undeclared.run = func(_ *Inputs, out *Outputs, _ Emitter) error { return out.Set(src(1)) }

	p, err = New(undeclared)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrNotDeclared) {
		t.Errorf("not declared: got %v", err)
	}

	notRequired := &fakeNode{name: "notrequired", requires: []KeyRef{Key[src]{}}}
	notRequired.run = func(in *Inputs, _ *Outputs, _ Emitter) error {
		_, err := in.Get[double]()
		return err
	}

	p, err = New(source(), notRequired)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background(), &fakeEmitter{}); !errors.Is(err, ErrNotInRequires) {
		t.Errorf("not in requires: got %v", err)
	}
}
