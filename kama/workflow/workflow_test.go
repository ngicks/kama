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
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
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

	if s.runs != 2 || dbl.runs != 2 {
		t.Errorf("source ran %d, doubler ran %d, want 2 and 2", s.runs, dbl.runs)
	}
	if want := []double{2, 4, 4}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
	if len(targets) != 1 {
		t.Fatalf("target seen %d times, want 1", len(targets))
	}
	if want := []string{"gen.go"}; !slices.Equal(targets[0].written, want) {
		t.Errorf("target written %v, want %v", targets[0].written, want)
	}
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

	if want := []src{1, 100}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
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

	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(defaultsMade) != 1 || defaultsMade[0].runs != 1 {
		t.Errorf("default constructed %d times, want 1", len(defaultsMade))
	}
	if want := []withDefault{7, 7}; !slices.Equal(seen, want) {
		t.Errorf("seen %v, want %v", seen, want)
	}
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
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); !errors.Is(err, ErrCycle) {
		t.Errorf("cycle: got %v", err)
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
	if err := p.Run(context.Background()); !errors.Is(err, ErrMissingOutput) {
		t.Errorf("missing output: got %v", err)
	}

	undeclared := &fakeNode{name: "undeclared"}
	undeclared.run = func(_ *Inputs, out *Outputs) error { return out.Set(src(1)) }

	p, err = New(undeclared)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background()); !errors.Is(err, ErrNotDeclared) {
		t.Errorf("not declared: got %v", err)
	}

	notRequired := &fakeNode{name: "notrequired", requires: []KeyRef{Key[src]{}}}
	notRequired.run = func(in *Inputs, _ *Outputs) error {
		_, err := in.Get[double]()
		return err
	}

	p, err = New(targetNode(), source(), notRequired)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Run(context.Background()); !errors.Is(err, ErrNotInRequires) {
		t.Errorf("not in requires: got %v", err)
	}
}
